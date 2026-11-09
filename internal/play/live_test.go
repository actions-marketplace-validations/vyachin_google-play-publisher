package play_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/vyachin/google-play-publisher/internal/play"
)

// TestLiveEditLifecycle talks to the real Play API. It is skipped unless
// GOOGLE_PLAY_PUBLISHER_INTEGRATION=1 and GOOGLE_PLAY_PUBLISHER_PACKAGE are set.
func TestLiveEditLifecycle(t *testing.T) {
	if os.Getenv("GOOGLE_PLAY_PUBLISHER_INTEGRATION") != "1" {
		t.Skip("set GOOGLE_PLAY_PUBLISHER_INTEGRATION=1 to run live Play API tests")
	}

	pkg := os.Getenv("GOOGLE_PLAY_PUBLISHER_PACKAGE")
	if pkg == "" {
		t.Skip("set GOOGLE_PLAY_PUBLISHER_PACKAGE to the test application id")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	client, err := play.New(ctx, play.Options{
		CredentialsFile: os.Getenv("GOOGLE_APPLICATION_CREDENTIALS"),
	})
	if err != nil {
		t.Fatalf("play.New: %v", err)
	}

	editID, err := client.CreateEdit(ctx, pkg)
	if err != nil {
		t.Fatalf("CreateEdit: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, done := context.WithTimeout(context.Background(), 30*time.Second)
		defer done()
		if err := client.DeleteEdit(cleanupCtx, pkg, editID); err != nil {
			t.Logf("DeleteEdit: %v", err)
		}
	})

	if _, err := client.TrackReleases(ctx, pkg, "internal"); err != nil {
		t.Fatalf("TrackReleases: %v", err)
	}
}
