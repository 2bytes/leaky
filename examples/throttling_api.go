// Example of using the throttling rate limiter
// Start a Redis instance in Docker like this: docker run -itd --name redis -p 6379:6379 redis:alpine
// Run this example with: go run throttling_api.go
// Make some requests and watch them throttle: curl http://localhost:7777/api
package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/2bytes/leaky"
	"github.com/go-redis/redis/v8"
)

func myEndpoint(w http.ResponseWriter, r *http.Request) {
	fmt.Printf("API called from: %q\n", r.RemoteAddr)
}

// Create a KeyFunc that will uniquely identify an incoming connection
// this is used to apply the rate limit to an individual client
// username or api token are good identifiers, remote IP is not.
func remoteIPKeyFunc(r http.Request) string {
	return strings.Split(r.RemoteAddr, ":")[0]
}

func main() {

	rc := redis.NewClient(&redis.Options{
		Addr:     "localhost:6379",
		Password: "",
		DB:       0,
	})

	if _, err := rc.Ping(context.Background()).Result(); err != nil {
		log.Printf("Failed to ping Redis server: %s\n", err)
		os.Exit(1)
	} else {
		log.Println("Connected to Redis instance")
	}

	tm := leaky.NewThrottleManager(rc)

	http.Handle("/api", tm.ThrottlingHandler(myEndpoint, 10, 60, remoteIPKeyFunc, "/api"))
	log.Println("Serving /api on :7777")
	http.ListenAndServe(":7777", nil)
}
