package app_test

import (
	"context"
	"testing"

	"github.com/vyachin/google-play-publisher/internal/app"
	"github.com/vyachin/google-play-publisher/internal/play/playtest"
)

func TestPublishSendsReleaseFields(t *testing.T) {
	fake := &playtest.Fake{}
	opts := baseOptions(writeAAB(t))
	opts.Track = "production"
	opts.Status = ""
	opts.Rollout = fraction(0.05)
	opts.InAppUpdatePriority = 4
	opts.Countries = []string{"de", "fr"}
	opts.IncludeRestOfWorld = true

	if _, err := app.NewPublisher(fake, nil).Publish(context.Background(), opts); err != nil {
		t.Fatalf("Publish: %v", err)
	}

	if len(fake.UpdatedReleases) != 1 {
		t.Fatalf("updated releases = %d, want 1", len(fake.UpdatedReleases))
	}
	updated := fake.UpdatedReleases[0]
	if updated.InAppUpdatePriority != 4 {
		t.Errorf("priority = %d, want 4", updated.InAppUpdatePriority)
	}
	if updated.CountryTargeting == nil {
		t.Fatalf("no country targeting in %+v", updated)
	}
	if len(updated.CountryTargeting.Countries) != 2 ||
		updated.CountryTargeting.Countries[0] != "DE" || updated.CountryTargeting.Countries[1] != "FR" {
		t.Errorf("countries = %v, want [DE FR]", updated.CountryTargeting.Countries)
	}
	if !updated.CountryTargeting.IncludeRestOfWorld {
		t.Error("include rest of world was dropped")
	}
	if updated.UserFraction != 0.05 {
		t.Errorf("fraction = %v, want 0.05", updated.UserFraction)
	}
}

func TestPublishRejectsCountryTargetingOffProduction(t *testing.T) {
	fake := &playtest.Fake{}
	opts := baseOptions(writeAAB(t))
	opts.Countries = []string{"DE"}

	if _, err := app.NewPublisher(fake, nil).Publish(context.Background(), opts); err == nil {
		t.Fatal("expected a usage error")
	}
	if len(fake.Calls) != 0 {
		t.Fatalf("API was called for invalid input: %v", fake.Calls)
	}
}
