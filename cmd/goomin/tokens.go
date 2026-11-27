package main

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/ahobsonsayers/goomin/garmin"
	"github.com/ahobsonsayers/goomin/google"
	"github.com/ahobsonsayers/goomin/state"
	"github.com/ahobsonsayers/goomin/syncer"
)

// errNotConnected means the provider has never been signed in.
var errNotConnected = errors.New("not connected")

func googleTokens(store *state.Store, client *google.Client) syncer.Tokens {
	return func(ctx context.Context, force bool) (state.Token, error) {
		stored := store.View().Google
		if stored.Connected() && !force && !stored.Expired() {
			return stored, nil
		}
		if stored.RefreshToken == "" {
			return state.Token{}, errNotConnected
		}

		fresh, err := client.Refresh(ctx, stored.RefreshToken)
		if err != nil {
			if errors.Is(err, google.ErrGrantDead) {
				clearToken(store, "google")
			}

			return state.Token{}, fmt.Errorf("refresh google token: %w", err)
		}

		return fresh, store.Update(func(data *state.Data) error {
			data.Google = fresh

			return nil
		})
	}
}

func garminTokens(store *state.Store) syncer.Tokens {
	return func(ctx context.Context, force bool) (state.Token, error) {
		stored := store.View().Garmin
		if stored.Connected() && !force && !stored.Expired() {
			return stored, nil
		}
		if stored.RefreshToken == "" {
			return state.Token{}, errNotConnected
		}

		fresh, err := garmin.Refresh(ctx, stored.RefreshToken)
		if err != nil {
			if strings.Contains(err.Error(), "invalid_grant") {
				clearToken(store, "garmin")
			}

			return state.Token{}, fmt.Errorf("refresh garmin token: %w", err)
		}
		if fresh.RefreshToken == "" {
			fresh.RefreshToken = stored.RefreshToken
		}

		return fresh, store.Update(func(data *state.Data) error {
			data.Garmin = fresh

			return nil
		})
	}
}

func clearToken(store *state.Store, provider string) {
	_ = store.Update(func(data *state.Data) error {
		switch provider {
		case "google":
			data.Google = state.Token{}
		case "garmin":
			data.Garmin = state.Token{}
		default:
			return fmt.Errorf("unknown provider %q", provider)
		}

		return nil
	})
	store.Logf("%s sign-in expired, connect it again", provider)
}
