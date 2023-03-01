# Leaky Bucket Throttling Rate Limiter v0.1.1 [![Go Report Card](https://goreportcard.com/badge/github.com/2bytes/leaky)](https://goreportcard.com/report/github.com/2bytes/leaky)

Implementation of the Leaky Bucket rate limiter as a Golang Middleware

## Prerequisites

* A running Redis instance to connect to and store real time state.
* A Redis client instance from [go-redis](https://github.com/go-redis/redis/v9)

Usage:

```
go get github.com/2bytes/leaky
```
Then import the middlware and go-redis
```
import (
    "github.com/2bytes/leaky"
    "github.com/redis/go-redis/v9"
)
```
Create your redis client instance as necessary, then instantiate the ThrottlingManager using it.

Limits can be set per handler, and each handler takes takes a KeyFunc used to identify a client.
```
rc := redis.NewClient(&redis.Options{
    Addr: "<redis server address>:6379",
    Password: "<redis password>",
    DB: <db number>,
})

// Consider pinging your Redis server here to ensure it's connected successfully

tm := leaky.NewThrottleManager(rc)

http.Handle("/api", tm.NewThrottlingHandler(myHandler, <bucket size>, <leak rate per minute>, keyFunc, "bucket name"))
```

## Bucket size
The number of requests a particular client can make before they start to be rate limited

## Leak rate
The rate at which a filled bucket leaks allowing more connections in that time period.

## Drop size
Currently the drop size is restricted to a unit drop size, since the bucket size and leak rate can be varied per endpoint, I currently see no need to complicate this with varied drop sizes.

## KeyFunc
The middleware provides an interface to provide your own key function, this is used to identify a particular client, by returning a string used to key the bucket values in the Redis database.

### Identifying clients
How you identify each client you wish to rate-limit is up to you, and depends entirely on your requirements.

Preferably, your API uses a username or token and you can use the key function to extract this from the necessary headers and construct a string to use as the key.

## Failure state
An implementation choice has been made that if the Redis instance is unavailable, the failure state is to reset the bucket counter to its  maximum size allowing requests to continue.

This happens per request, and if the server returns, the state will be returned to its previous value (taking into account elapsed time).