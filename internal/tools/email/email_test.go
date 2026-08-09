package email

import "testing"

func TestValidateConfig(t *testing.T) {
	valid := &config{
		SmtpHost:    "smtp.example.com",
		SmtpPort:    465,
		Username:    "sender@example.com",
		Password:    "app-password",
		FromAddress: "Sender <sender@example.com>",
	}
	host, port, auth, err := validateConfig(valid)
	if err != nil {
		t.Fatal(err)
	}
	if host != "smtp.example.com" || port != 465 || auth == nil {
		t.Fatalf("validateConfig() = (%q, %d, %T), want smtp config with auth", host, port, auth)
	}
}

func TestValidateConfigRejectsIncompleteAuthentication(t *testing.T) {
	_, _, _, err := validateConfig(&config{
		SmtpHost:    "smtp.example.com",
		SmtpPort:    587,
		Username:    "sender@example.com",
		FromAddress: "sender@example.com",
	})
	if err == nil {
		t.Fatal("validateConfig() accepted incomplete authentication")
	}
}

func TestValidateInputRejectsInvalidRecipient(t *testing.T) {
	err := validateInput(&input{Html: "<p>hello</p>", To: []string{"not-an-address"}})
	if err == nil {
		t.Fatal("validateInput() accepted invalid recipient")
	}
}
