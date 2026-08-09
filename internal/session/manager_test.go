package session

import (
	"context"
	"testing"
	"time"

	"github.com/cloudwego/eino/schema"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func TestTrimRounds(t *testing.T) {
	messages := []*schema.Message{
		schema.UserMessage("one"),
		schema.AssistantMessage("first", nil),
		schema.UserMessage("two"),
		schema.AssistantMessage("second", nil),
		schema.UserMessage("three"),
		schema.AssistantMessage("third", nil),
	}

	got := trimRounds(messages, 2)
	if len(got) != 4 || got[0].Content != "two" {
		t.Fatalf("trimRounds() = %#v", got)
	}
}

func TestSaveLoadSeparatesCharactersAndDropsDynamicSystem(t *testing.T) {
	db := openTestDB(t)
	manager := New(db)
	var err error
	if err = manager.Init(); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err = manager.Save(ctx, 1, "character.one", 36, []*schema.Message{
		schema.SystemMessage("dynamic"),
		schema.UserMessage("hello"),
	}); err != nil {
		t.Fatal(err)
	}
	if err = manager.Save(ctx, 1, "character.two", 36, []*schema.Message{
		schema.UserMessage("other"),
	}); err != nil {
		t.Fatal(err)
	}
	one, err := manager.Load(ctx, 1, "character.one", 36)
	if err != nil {
		t.Fatal(err)
	}
	two, err := manager.Load(ctx, 1, "character.two", 36)
	if err != nil {
		t.Fatal(err)
	}
	if len(one) != 1 || one[0].Role != schema.User || one[0].Content != "hello" {
		t.Fatalf("character one history = %#v", one)
	}
	if len(two) != 1 || two[0].Content != "other" {
		t.Fatalf("character two history = %#v", two)
	}
	var count int64
	if err = db.Model(&Record{}).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("stored record count = %d, want one row per message (2)", count)
	}
}

func TestLoadOrdersMessagesByCreationTimeThenID(t *testing.T) {
	db := openTestDB(t)
	manager := New(db)
	if err := manager.Init(); err != nil {
		t.Fatal(err)
	}

	later, err := makeRecords(1, "character.one", []*schema.Message{schema.AssistantMessage("later", nil)})
	if err != nil {
		t.Fatal(err)
	}
	earlier, err := makeRecords(1, "character.one", []*schema.Message{schema.UserMessage("earlier")})
	if err != nil {
		t.Fatal(err)
	}
	later[0].CreatedAt = time.Unix(200, 0)
	earlier[0].CreatedAt = time.Unix(100, 0)
	if err = db.Create(&later).Error; err != nil {
		t.Fatal(err)
	}
	if err = db.Create(&earlier).Error; err != nil {
		t.Fatal(err)
	}

	messages, err := manager.Load(context.Background(), 1, "character.one", 36)
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 2 || messages[0].Content != "earlier" || messages[1].Content != "later" {
		t.Fatalf("chronological messages = %#v", messages)
	}
}

func TestAppendStoresRowsAndTrimsOldRounds(t *testing.T) {
	db := openTestDB(t)
	manager := New(db)
	if err := manager.Init(); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	for _, turn := range []struct {
		user      string
		assistant string
	}{
		{user: "one", assistant: "first"},
		{user: "two", assistant: "second"},
		{user: "three", assistant: "third"},
	} {
		if err := manager.Append(ctx, 1, "character.one", 2,
			schema.UserMessage(turn.user),
			schema.AssistantMessage(turn.assistant, nil),
		); err != nil {
			t.Fatal(err)
		}
	}

	messages, err := manager.Load(ctx, 1, "character.one", 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 4 || messages[0].Content != "two" || messages[3].Content != "third" {
		t.Fatalf("trimmed messages = %#v", messages)
	}
	var count int64
	if err = db.Model(&Record{}).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != int64(len(messages)) {
		t.Fatalf("stored record count = %d, loaded messages = %d", count, len(messages))
	}
}

func TestAppendUsesPerUserMaxRounds(t *testing.T) {
	db := openTestDB(t)
	manager := New(db)
	if err := manager.Init(); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	for _, userID := range []int64{1, 2} {
		for _, text := range []string{"one", "two", "three"} {
			maxRounds := 1
			if userID == 2 {
				maxRounds = 2
			}
			if err := manager.Append(ctx, userID, "character.one", maxRounds,
				schema.UserMessage(text),
				schema.AssistantMessage(text, nil),
			); err != nil {
				t.Fatal(err)
			}
		}
	}

	first, err := manager.Load(ctx, 1, "character.one", 1)
	if err != nil {
		t.Fatal(err)
	}
	second, err := manager.Load(ctx, 2, "character.one", 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 2 || first[0].Content != "three" {
		t.Fatalf("first user history = %#v", first)
	}
	if len(second) != 4 || second[0].Content != "two" {
		t.Fatalf("second user history = %#v", second)
	}
}

func TestWithoutLeadingSystemKeepsLaterSystemMessages(t *testing.T) {
	messages := []*schema.Message{
		schema.SystemMessage("dynamic"),
		schema.UserMessage("hello"),
		schema.SystemMessage("keep"),
	}

	got := withoutLeadingSystem(messages)
	if len(got) != 2 || got[1].Role != schema.System {
		t.Fatalf("withoutLeadingSystem() = %#v", got)
	}
}

func openTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"))
	if err != nil {
		t.Fatal(err)
	}
	return db
}
