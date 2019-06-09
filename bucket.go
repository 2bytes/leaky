// Package leaky provides a simple implementation of the leaky bucket algorithm
// It can be used as a middleware or called manually depending on requirements
//
// At the moment, the leaky-bucket and handler middleware are tightly coupled,
// this should be cleaned up to allow usage in scenarios where the handler middleware
// is not required, or not desired.
//
// At the moment, the middleware and the leaky-bucket are tightly coupled to Redis as a cache,
// although a Redis failure is non-fatal (fail-open), the dependency might not be required
// or desired, and should be decoupled appropriately.
package leaky

import (
	"encoding/json"
	"fmt"
	"log"
	"math"
	"net/http"
	"time"

	"github.com/go-redis/redis"
)

// Handler creates a new rate limiting leaky bucket handler
type Handler func(w http.ResponseWriter, r *http.Request)

// KeyFunc allows an implementation to use a request value to identify a client
type KeyFunc func(r http.Request) string

// ThrottleManager manages leaky buckets
type ThrottleManager struct {
	redis *redis.Client
}

type bucketState struct {
	LastUpdate     time.Time `json:"last_update"`
	SpaceRemaining float64   `json:"space_remaining"`
}

func (s bucketState) MarshalBinary() ([]byte, error) {
	return json.Marshal(s)
}

func (s *bucketState) UnmarshalBinary(data []byte) error {
	return json.Unmarshal(data, s)
}

// Bucket is the instance of a leaky bucket
type Bucket struct {
	size       int
	state      bucketState
	leakRate   float64
	bucketName string
	handler    Handler
	keyFunc    KeyFunc
	redis      *redis.Client
}

func (b *Bucket) getKey(keyID string) string {
	return fmt.Sprintf("leaky::%s::%s", b.bucketName, keyID)
}

func (b *Bucket) setState(updatedState bucketState, keyID string) {

	if err := b.redis.Set(b.getKey(keyID), updatedState, time.Hour).Err(); err != nil && err != redis.Nil {
		log.Printf("Setting bucket state failed: %q\n", err)
	}
}

func (b *Bucket) getState(keyID string) bucketState {

	lastState := bucketState{}

	if err := b.redis.Get(b.getKey(keyID)).Scan(&lastState); err != nil {
		if err != redis.Nil {
			log.Printf("Retrieving bucket state failed, resetting counters: %s\n", err)
		}

		lastState.SpaceRemaining = float64(b.size)
		lastState.LastUpdate = time.Now()
		return lastState
	}

	// Calculate how much the bucket has leaked and update the cache
	elapsed := float64(time.Since(lastState.LastUpdate) / time.Millisecond)
	newRemaining := math.Floor(lastState.SpaceRemaining + (elapsed * float64(b.leakRate)))

	updatedState := bucketState{
		SpaceRemaining: math.Min(float64(b.size), newRemaining),
		LastUpdate:     time.Now(),
	}

	return updatedState
}

func (b *Bucket) fill(count int, keyID string) bool {
	// First update our bucket state; time has passed so some drops have leaked
	currState := b.getState(keyID)

	// Return whether we have space for the number of drops we've been asked to add to the bucket
	if currState.SpaceRemaining < float64(count) {
		return false
	}

	b.setState(bucketState{SpaceRemaining: currState.SpaceRemaining - float64(count), LastUpdate: time.Now()}, keyID)
	return true
}

// Add adds drops to the bucket if there is space
func (b *Bucket) Add(count int, keyID string) bool {

	if b.fill(count, keyID) {
		return true
	}

	return false
}

// NewBucket creates a new bucket directly if you're not using the handler
func (m *ThrottleManager) newBucket(handler Handler, size int, leakRatePerMin int, keyFunc KeyFunc, bucketName string) *Bucket {
	bucket := &Bucket{
		size:       size,
		leakRate:   float64(leakRatePerMin) / (60.0 * 1000.0),
		handler:    handler,
		keyFunc:    keyFunc,
		redis:      m.redis,
		bucketName: bucketName,
	}

	return bucket
}

// ServeHTTP implements http.Handler
func (b *Bucket) ServeHTTP(w http.ResponseWriter, r *http.Request) {

	if b.Add(1, b.keyFunc(*r)) {
		b.handler(w, r)
	} else {
		http.Error(w, "Rate Limit Exceeded", http.StatusTooManyRequests)
	}
}

// ThrottlingHandler creates a new handler wrapper for use as an HTTP middleware
func (m *ThrottleManager) ThrottlingHandler(handler Handler, size int, rate int, keyFunc KeyFunc, bucketName string) *Bucket {
	return m.newBucket(handler, size, rate, keyFunc, bucketName)
}

// NewThrottleManager creates a new instance of bucket manager
// it requires a Redis client for storing state
func NewThrottleManager(redis *redis.Client) *ThrottleManager {
	bm := &ThrottleManager{
		redis: redis,
	}

	return bm
}
