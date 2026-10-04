package store

import (
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestReasoningEffortMigrationPreservesExistingModel(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(t.TempDir()+"/legacy.db"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := db.Exec(`CREATE TABLE ai_models (id TEXT PRIMARY KEY, user_id TEXT NOT NULL DEFAULT 'default', name TEXT NOT NULL, provider TEXT NOT NULL, enabled NUMERIC DEFAULT 0, api_key TEXT DEFAULT '', custom_api_url TEXT DEFAULT '', custom_model_name TEXT DEFAULT '', created_at DATETIME, updated_at DATETIME)`).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`INSERT INTO ai_models (id,user_id,name,provider,custom_model_name) VALUES ('alice_codex','alice','Existing','codex','gpt-5.6-luna')`).Error; err != nil {
		t.Fatal(err)
	}
	s := NewAIModelStore(db)
	if err := s.initTables(); err != nil {
		t.Fatal(err)
	}
	model, err := s.Get("alice", "alice_codex")
	if err != nil || model.CustomModelName != "gpt-5.6-luna" || model.ReasoningEffort != "" {
		t.Fatalf("legacy migration changed config: %v", err)
	}
}

func TestReasoningEffortPersistsAndLegacyUpdatesPreserveIt(t *testing.T) {
	st, err := New(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	s := st.AIModel()
	if err := s.Update("alice", "codex", false, "", "", "gpt-6.1-sol", "high"); err != nil {
		t.Fatal(err)
	}
	if err := s.Update("alice", "alice_codex", false, "", "", "gpt-6.1-sol"); err != nil {
		t.Fatal(err)
	}
	model, err := s.Get("alice", "alice_codex")
	if err != nil || model.ReasoningEffort != "high" {
		t.Fatalf("omitted setting was lost: %v", err)
	}
	if err := s.Update("alice", "codex", false, "", "", "gpt-6-sol", "none"); err != nil {
		t.Fatal(err)
	}
	model, err = s.Get("alice", "alice_codex")
	if err != nil || model.ReasoningEffort != "none" || model.CustomModelName != "gpt-6-sol" {
		t.Fatalf("legacy lookup lost effort: %v", err)
	}
}
