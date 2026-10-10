package cache

import (
	"context"
	"encoding/json"
	"time"

	"golang.org/x/sync/singleflight"
)

// fetches joins the callers that compute the same key at the same time.
var fetches singleflight.Group

// Fetch returns the value stored under key. When the store has none, it
// calls fn, stores the result as JSON, and returns it, like
// Rails.cache.fetch:
//
//	stats, err := cache.Fetch(ctx, store, "stats:"+siteID, time.Minute, func() (Stats, error) {
//	    return loadStats(siteID)
//	})
//
// A ttl of 0 uses the default TTL of the store. When many callers miss the
// same key at the same time, fn runs once and all of them get its result.
// An error from fn is returned and not stored. A value that the store holds
// but that does not decode into T counts as a miss.
func Fetch[T any](ctx context.Context, store Store, key string, ttl time.Duration, fn func() (T, error)) (T, error) {
	if value, ok := read[T](ctx, store, key); ok {
		return value, nil
	}

	result, err, _ := fetches.Do(key, func() (any, error) {
		// Another caller can have stored the value while this one waited.
		if value, ok := read[T](ctx, store, key); ok {
			return value, nil
		}
		value, err := fn()
		if err != nil {
			return value, err
		}
		data, err := json.Marshal(value)
		if err != nil {
			return value, err
		}
		if ttl > 0 {
			err = store.WriteWithTTL(ctx, key, data, ttl)
		} else {
			err = store.Write(ctx, key, data)
		}
		// A store that cannot write must not fail the request: the value is good.
		_ = err
		return value, nil
	})
	if err != nil {
		var zero T
		return zero, err
	}
	// Two callers with the same key and another T would share a result.
	value, ok := result.(T)
	if !ok {
		return fn()
	}
	return value, nil
}

func read[T any](ctx context.Context, store Store, key string) (T, bool) {
	var value T
	data, ok := store.Read(ctx, key)
	if !ok || json.Unmarshal(data, &value) != nil {
		var zero T
		return zero, false
	}
	return value, true
}
