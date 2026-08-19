package main

import "testing"

func TestValidateTestDatabaseURL(t *testing.T) {
	t.Parallel()

	valid := "postgres://postgres:secret@127.0.0.1:5432/carry_test_20260818153000_abcdef123456_postgres?sslmode=disable"
	if err := validateTestDatabaseURL(valid); err != nil {
		t.Fatalf("valid URL rejected: %v", err)
	}
	for _, unsafe := range []string{
		"postgres://postgres:secret@database.example.com/carry_test_20260818153000_abcdef123456_postgres",
		"postgres://postgres:secret@127.0.0.1/production",
		"https://127.0.0.1/carry_test_20260818153000_abcdef123456_postgres",
	} {
		if err := validateTestDatabaseURL(unsafe); err == nil {
			t.Errorf("unsafe URL accepted: %s", unsafe)
		}
	}
}
