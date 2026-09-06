package metadata

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestActivityLifecycle(t *testing.T) {
	ctx := context.Background()
	store, _ := openTestStore(t)

	database, err := store.CreateDatabaseMetadata(
		ctx,
		"Scheduler Production",
	)
	if err != nil {
		t.Fatalf(
			"CreateDatabaseMetadata() error = %v",
			err,
		)
	}

	baseTime := time.Date(
		2026,
		time.September,
		6,
		20,
		0,
		0,
		0,
		time.UTC,
	)

	store.now = func() time.Time {
		return baseTime
	}

	first, err := store.CreateActivity(
		ctx,
		ActivityEventInput{
			DatabaseID:          database.ID,
			DatabaseDisplayName: database.DisplayName,
			Type:                ActivityDatabaseCreate,
			Outcome:             ActivitySuccess,
			Source:              ActivitySourceAdmin,
			Detail:              "Database created successfully.",
		},
	)
	if err != nil {
		t.Fatalf(
			"CreateActivity() error = %v",
			err,
		)
	}

	store.now = func() time.Time {
		return baseTime.Add(time.Minute)
	}

	second, err := store.CreateActivity(
		ctx,
		ActivityEventInput{
			Type:    ActivityAutomaticBackup,
			Outcome: ActivitySuccess,
			Source:  ActivitySourceSystem,
			Detail:  "Automatic backup check completed.",
		},
	)
	if err != nil {
		t.Fatalf(
			"CreateActivity(global) error = %v",
			err,
		)
	}

	listed, err := store.ListActivity(ctx, 100)
	if err != nil {
		t.Fatalf(
			"ListActivity() error = %v",
			err,
		)
	}

	if len(listed) != 2 {
		t.Fatalf(
			"ListActivity() len = %d, want 2",
			len(listed),
		)
	}

	if listed[0] != second {
		t.Fatalf(
			"newest event = %#v, want %#v",
			listed[0],
			second,
		)
	}

	if listed[1] != first {
		t.Fatalf(
			"older event = %#v, want %#v",
			listed[1],
			first,
		)
	}

	databaseEvents, err :=
		store.ListActivityForDatabase(
			ctx,
			database.ID,
			100,
		)
	if err != nil {
		t.Fatalf(
			"ListActivityForDatabase() error = %v",
			err,
		)
	}

	if len(databaseEvents) != 1 ||
		databaseEvents[0] != first {
		t.Fatalf(
			"database activity = %#v",
			databaseEvents,
		)
	}

	if err := store.DeleteDatabaseMetadata(
		ctx,
		database.ID,
	); err != nil {
		t.Fatalf(
			"DeleteDatabaseMetadata() error = %v",
			err,
		)
	}

	databaseEvents, err =
		store.ListActivityForDatabase(
			ctx,
			database.ID,
			100,
		)
	if err != nil {
		t.Fatalf(
			"activity after deletion error = %v",
			err,
		)
	}

	if len(databaseEvents) != 1 ||
		databaseEvents[0].DatabaseID != database.ID {
		t.Fatalf(
			"activity was not preserved after deletion: %#v",
			databaseEvents,
		)
	}
}

func TestActivityValidation(t *testing.T) {
	ctx := context.Background()
	store, _ := openTestStore(t)

	_, err := store.CreateActivity(
		ctx,
		ActivityEventInput{
			Type:    ActivityType("unknown"),
			Outcome: ActivitySuccess,
			Source:  ActivitySourceAdmin,
			Detail:  "Invalid event.",
		},
	)
	if !errors.Is(err, ErrInvalidActivity) {
		t.Fatalf(
			"invalid type error = %v",
			err,
		)
	}

	_, err = store.CreateActivity(
		ctx,
		ActivityEventInput{
			Type:    ActivityDatabaseCreate,
			Outcome: ActivitySuccess,
			Source:  ActivitySourceAdmin,
			Detail:  "   ",
		},
	)
	if !errors.Is(err, ErrInvalidActivity) {
		t.Fatalf(
			"empty detail error = %v",
			err,
		)
	}
}
