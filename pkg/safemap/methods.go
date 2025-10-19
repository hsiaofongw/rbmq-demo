package safemap

import (
	"errors"
)

// ErrMapClosed is returned when an operation is attempted on a closed SafeMap.
var ErrMapClosed = errors.New("safemap: map is closed")

// NewSafeMap creates and initializes a new SafeMap instance.
// It starts the central actor goroutine that will manage all map operations.
func NewSafeMap() *SafeMap {
	sm := &SafeMap{
		store:      make(map[string]interface{}),
		requestsCh: make(chan func()),
		closeCh:    make(chan struct{}),
	}

	// Start the actor goroutine.
	go sm.run()

	return sm
}

// run is the central actor goroutine. It owns the 'store' map and is the only
// goroutine allowed to access it directly. It waits for requests (as functions)
// from the requestsCh and executes them sequentially.
func (sm *SafeMap) run() {
	// The loop continues until a close signal is received.
	for {
		select {
		// A request is received. A request is a function that will be executed.
		case req := <-sm.requestsCh:
			req()
		// The close signal is received.
		case <-sm.closeCh:
			// Set the closed flag and close the channel to signal completion.
			sm.closed = true
			close(sm.requestsCh)
			return
		}
	}
}

// Get retrieves a value from the map. It is thread-safe.
// The provided 'op' function is executed safely within the actor's context,
// preventing data races if the retrieved value is a pointer or a reference type.
func (sm *SafeMap) Get(key string, op func(value interface{}) error) (err error, found bool) {
	// A response channel to get the results back from the actor goroutine.
	respCh := make(chan struct {
		err   error
		found bool
	})

	// Create the request function.
	req := func() {
		if sm.closed {
			respCh <- struct {
				err   error
				found bool
			}{err: ErrMapClosed, found: false}
			return
		}
		var err error
		var found bool
		if v, ok := sm.store[key]; ok {
			err = op(v)
			found = true
		}
		respCh <- struct {
			err   error
			found bool
		}{err: err, found: found}
	}

	// Send the request and wait for the response.
	sm.requestsCh <- req
	resp := <-respCh
	return resp.err, resp.found
}

// Set adds or updates a key-value pair in the map. It is thread-safe.
func (sm *SafeMap) Set(key string, value interface{}) {
	// A channel to signal that the operation is complete.
	done := make(chan struct{})

	// Create the request function.
	req := func() {
		if !sm.closed {
			sm.store[key] = value
		}
		close(done)
	}

	// Send the request and wait for it to complete.
	sm.requestsCh <- req
	<-done
}

// Delete removes a key from the map. It is thread-safe.
func (sm *SafeMap) Delete(key string) {
	done := make(chan struct{})

	req := func() {
		if !sm.closed {
			delete(sm.store, key)
		}
		close(done)
	}

	sm.requestsCh <- req
	<-done
}

// Len returns the number of items in the map. It is thread-safe.
func (sm *SafeMap) Len() int {
	respCh := make(chan int)

	req := func() {
		var length int
		if !sm.closed {
			length = len(sm.store)
		}
		respCh <- length
	}

	sm.requestsCh <- req
	return <-respCh
}

// Dump creates a shallow copy of the map. It is thread-safe.
// It takes a 'cloner' function to create copies of the values, ensuring that
// the caller does not get direct references to the values stored in the map,
// which would break thread safety. If cloner is nil, values are copied directly.
func (sm *SafeMap) Dump(cloner func(value interface{}) interface{}) map[string]interface{} {
	respCh := make(chan map[string]interface{})

	req := func() {
		if sm.closed {
			respCh <- nil
			return
		}

		result := make(map[string]interface{}, len(sm.store))
		for k, v := range sm.store {
			if cloner != nil {
				result[k] = cloner(v)
			} else {
				result[k] = v
			}
		}
		respCh <- result
	}

	sm.requestsCh <- req
	return <-respCh
}

// Close shuts down the SafeMap's actor goroutine.
// Any subsequent attempts to use the map will fail. It is safe to call Close multiple times.
func (sm *SafeMap) Close() {
	sm.closeMutex.Lock()
	defer sm.closeMutex.Unlock()

	if sm.closed {
		return
	}
	close(sm.closeCh)
}
