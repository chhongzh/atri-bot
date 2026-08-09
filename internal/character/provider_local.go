package character

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

type LocalProvider struct {
	id   string
	root string
}

func NewLocalProvider(id, root string) *LocalProvider {
	return &LocalProvider{id: id, root: root}
}

func (p *LocalProvider) ID() string {
	return p.id
}

func (p *LocalProvider) Load(_ context.Context) ([]*Character, error) {
	entries, err := os.ReadDir(p.root)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })

	characters := make([]*Character, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		extension := strings.ToLower(filepath.Ext(entry.Name()))
		if extension != ".yaml" && extension != ".yml" {
			continue
		}
		path := filepath.Join(p.root, entry.Name())
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return nil, readErr
		}
		definition := make(map[string]any)
		if unmarshalErr := yaml.Unmarshal(data, &definition); unmarshalErr != nil {
			return nil, fmt.Errorf("parse character %s: %w", path, unmarshalErr)
		}
		id := strings.TrimSuffix(entry.Name(), extension)
		characters = append(characters, &Character{
			ID:         id,
			ProviderID: p.id,
			Definition: definition,
			Source:     path,
		})
	}
	return characters, nil
}
