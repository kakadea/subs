package store

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	_ "github.com/go-sql-driver/mysql"
)

func TestMariaDBIntegration(t *testing.T) {
	dsn := os.Getenv("SUBS_TEST_DSN")
	if dsn == "" {
		t.Skip("set SUBS_TEST_DSN to run MariaDB integration tests")
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"audit_logs", "download_links", "subtitles", "sessions", "users"} {
		if _, err := db.ExecContext(ctx, "DROP TABLE IF EXISTS "+table); err != nil {
			t.Fatal(err)
		}
	}
	st := New(db)
	if err := st.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if err := st.EnsureAdmin(ctx, "admin@test.local", "a-very-long-test-password"); err != nil {
		t.Fatal(err)
	}
	admin, err := st.Authenticate(ctx, "admin@test.local", "a-very-long-test-password")
	if err != nil || !admin.IsAdmin() {
		t.Fatalf("authenticate admin: user=%+v err=%v", admin, err)
	}
	session, err := st.CreateSession(ctx, admin.ID, time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.GetSessionUser(ctx, session); err != nil {
		t.Fatal(err)
	}
	sub := Subtitle{PublicID: "0123456789abcdef0123456789abcdef", Title: "Test", Format: "srt", OriginalFilename: "test.srt", StorageName: "0123456789012345678901234567890123456789012345678901234567890123.srt", StoragePath: "subtitles/01/23/file.srt", FileSize: 42, Checksum: "0123456789012345678901234567890123456789012345678901234567890123", Version: "1.0", Visibility: "public", CreatedBy: admin.ID}
	if err := st.CreateSubtitle(ctx, sub); err != nil {
		t.Fatal(err)
	}
	got, err := st.GetSubtitle(ctx, sub.PublicID, false)
	if err != nil || got.Title != sub.Title {
		t.Fatalf("get subtitle: got=%+v err=%v", got, err)
	}
	link, err := st.CreateDownloadLink(ctx, got.ID, admin.ID, time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.GetDownloadLink(ctx, link); err != nil {
		t.Fatal(err)
	}
	if err := st.DeleteSubtitle(ctx, sub.PublicID); err != nil {
		t.Fatal(err)
	}
	if _, err := st.GetSubtitle(ctx, sub.PublicID, true); err != ErrNotFound {
		t.Fatalf("expected deleted subtitle to be hidden, got %v", err)
	}
}
