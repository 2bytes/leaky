// Tests for the leaky package
// Some of these tests are time based an raise some interesting questions about using
// actual time for testing. If it causes issues on different systems, I'll consider implementing
// TimeProvider to fake the times for a more reproducible test.
package leaky

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

var (
	testKey = "leaky::test::test-key"
)

func handleFuncSuccessResponse(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
}

func keyFunc(r http.Request) string {
	return "test-key"
}

type Jig struct {
	ThrottleManager *ThrottleManager
	miniRedis       *miniredis.Miniredis
}

func prepareTestJig() *Jig {

	mr, err := miniredis.Run()
	if err != nil {
		panic(err)
	}

	rc := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	tm := NewThrottleManager(rc)

	jig := &Jig{
		ThrottleManager: tm,
		miniRedis:       mr,
	}

	return jig
}

func (tj *Jig) Close() {
	tj.miniRedis.Close()
}

func TestZeroSizeBucket(t *testing.T) {

	tj := prepareTestJig()
	defer tj.Close()

	handler := tj.ThrottleManager.ThrottlingHandler(handleFuncSuccessResponse, 0, 10, keyFunc, "test")
	req, _ := http.NewRequest("GET", "", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusTooManyRequests {
		t.Errorf("Error code: %v", w.Code)
	}

}

func TestZeroLeakBucket(t *testing.T) {

	tj := prepareTestJig()
	defer tj.Close()

	handler := tj.ThrottleManager.ThrottlingHandler(handleFuncSuccessResponse, 1, 0, keyFunc, "test")
	req, _ := http.NewRequest("GET", "", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Status not OK: %v\n", w.Code)
	}

	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusTooManyRequests {
		t.Errorf("Status not TooManyRequests: %v\n", w.Code)
	}
}

func TestThrottleOn(t *testing.T) {
	tj := prepareTestJig()
	defer tj.Close()

	handler := tj.ThrottleManager.ThrottlingHandler(handleFuncSuccessResponse, 1, 10, keyFunc, "test")
	req, _ := http.NewRequest("GET", "", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Status not OK: %v\n", w.Code)
	}

	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusTooManyRequests {
		t.Errorf("Status not TooManyRequests: %v\n", w.Code)
	}
}

func TestThrottleOnOff(t *testing.T) {
	tj := prepareTestJig()
	defer tj.Close()

	// With a leak rate of 600/min (10/s) we should be able to add another drop to the bucket after ~100 milliseconds
	handler := tj.ThrottleManager.ThrottlingHandler(handleFuncSuccessResponse, 1, 600, keyFunc, "test")
	req, _ := http.NewRequest("GET", "", nil)

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Status not OK: %v\n", w.Code)
	}

	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusTooManyRequests {
		t.Errorf("Status not TooManyRequests: %v\n", w.Code)
	}

	// We've added some margin (50%) to be safe
	time.Sleep(time.Millisecond * 150)

	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Status not OK: %v\n", w.Code)
	}
}

func TestStickyThrottle(t *testing.T) {
	tj := prepareTestJig()
	defer tj.Close()

	// With a leak rate of 600/min (10/s) we should be able to add another drop to the bucket after ~100 milliseconds
	handler := tj.ThrottleManager.ThrottlingHandler(handleFuncSuccessResponse, 1, 600, keyFunc, "test")
	req, _ := http.NewRequest("GET", "", nil)

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Status not OK: %v\n", w.Code)
	}

	// If we add drops at a rate faster than the leak rate, we should be throttled until we slow down
	// The bucket should allow ~10 of our drops through at this rate
	var leakCount int

	for i := 0; i <= 100; i++ {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)

		if w.Code != http.StatusTooManyRequests {
			leakCount++
		}

		time.Sleep(time.Millisecond * 10)
	}

	if leakCount > 10 {
		t.Error("Bucket leaked too many drops")
	}
}

func TestGetKey(t *testing.T) {
	tj := prepareTestJig()
	defer tj.Close()

	// With a leak rate of 600/min (10/s) we should be able to add another drop to the bucket after ~100 milliseconds
	handler := tj.ThrottleManager.ThrottlingHandler(handleFuncSuccessResponse, 1, 600, keyFunc, "test")
	genKey := handler.getKey("test-key")

	if strings.Compare(genKey, testKey) != 0 {
		t.Errorf("key comparison failed: %s != %s\n", genKey, testKey)
	}
}

func TestAddToZeroSizeBucket(t *testing.T) {
	tj := prepareTestJig()
	defer tj.Close()

	handler := tj.ThrottleManager.ThrottlingHandler(handleFuncSuccessResponse, 0, 0, keyFunc, "test")

	if ok := handler.Add(1, "test"); ok {
		t.Error("Added drop to zero size bucket")
	}
}

func TestOverflowBucket(t *testing.T) {
	tj := prepareTestJig()
	defer tj.Close()

	handler := tj.ThrottleManager.ThrottlingHandler(handleFuncSuccessResponse, 10, 0, keyFunc, "test")

	if ok := handler.Add(11, "test"); ok {
		t.Error("Bucket overflow")
	}
}

func TestGetStateSpaceRemaining(t *testing.T) {
	tj := prepareTestJig()
	defer tj.Close()

	handler := tj.ThrottleManager.ThrottlingHandler(handleFuncSuccessResponse, 43, 0, keyFunc, "test")

	testBucketState := bucketState{
		LastUpdate:     time.Now(),
		SpaceRemaining: 43,
	}

	data, err := testBucketState.MarshalBinary()
	if err != nil {
		t.Error("Error marshalling state to JSON")
	}

	tj.miniRedis.Set(testKey, string(data))

	bucketState := handler.getState(testKey)

	if testBucketState.SpaceRemaining != bucketState.SpaceRemaining {
		t.Errorf("Bucket space remaining doesn't match: %f, %f", testBucketState.SpaceRemaining, bucketState.SpaceRemaining)
	}
}

func TestSetStateFail(t *testing.T) {
	tj := prepareTestJig()
	tj.Close()

	handler := tj.ThrottleManager.ThrottlingHandler(handleFuncSuccessResponse, 43, 0, keyFunc, "test")

	handler.setState(bucketState{LastUpdate: time.Now(), SpaceRemaining: 43}, testKey)
}

func TestGetStateFail(t *testing.T) {
	tj := prepareTestJig()
	tj.Close()

	handler := tj.ThrottleManager.ThrottlingHandler(handleFuncSuccessResponse, 43, 0, keyFunc, "test")

	bucketState := handler.getState(testKey)

	if bucketState.SpaceRemaining != 43 {
		t.Error("Failure to get state was critical")
	}

}
