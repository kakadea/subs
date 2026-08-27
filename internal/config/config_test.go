package config

import "testing"

func TestLoadRequiresSessionSecret(t *testing.T) {
	t.Setenv("SESSION_SECRET", "short")
	if _, err := Load(); err == nil {
		t.Fatal("expected short session secret to be rejected")
	}
}

func TestLoadAcceptsSecureDefaults(t *testing.T) {
	t.Setenv("SESSION_SECRET", "01234567890123456789012345678901")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("expected valid config: %v", err)
	}
	if cfg.MaxUploadBytes != 25*1024*1024 {
		t.Fatalf("unexpected default upload limit: %d", cfg.MaxUploadBytes)
	}
	if cfg.SessionTTL.Hours() != 168 {
		t.Fatalf("unexpected session ttl: %v", cfg.SessionTTL)
	}
}
