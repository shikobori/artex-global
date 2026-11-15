package server

import (
	"os"
	"path/filepath"
	"testing"
)

// Regression coverage for English archive-package deletion preparation errors.
// The archive worker stores these errors in task_archives.error, which the task
// archive panel displays directly. The shared assertion checks the message language.
//
// stageTaskArchivePackageDelete needs no database, so creating both the original
// and .deleting-N files exercises the inconsistent-state branch directly.
func TestTaskArchiveStagedAndOriginalCoexistErrorLocalized(t *testing.T) {
	dataDir := t.TempDir()
	archivePath := taskArchivePath(dataDir, 21, "42")
	if err := os.MkdirAll(filepath.Dir(archivePath), archiveDirMode); err != nil {
		t.Fatal(err)
	}
	// Create the inconsistent state with both the original and staged deletion file.
	if err := os.WriteFile(archivePath, []byte("original"), archiveFileMode); err != nil {
		t.Fatal(err)
	}
	staged := archivePath + ".deleting-21"
	if err := os.WriteFile(staged, []byte("archive"), archiveFileMode); err != nil {
		t.Fatal(err)
	}
	_, moved, err := stageTaskArchivePackageDelete(archivePath, 21)
	if err == nil {
		t.Fatalf("an error is required when the original and staged deletion file coexist (moved=%v)", moved)
	}
	if moved {
		t.Fatal("moved must not be true when the original and staged deletion file coexist")
	}
	assertEnglishError(t, "staged_and_original_coexist", err.Error())
}
