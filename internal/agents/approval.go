package agents

import (
	"sync"
	"time"
)

// ApprovalRequest represents a pending operation requiring user approval.
type ApprovalRequest struct {
	ID        string
	Method    string
	Params    map[string]any
	Timestamp time.Time
	Response  chan bool
}

// ApprovalQueue manages pending approval requests for write operations.
type ApprovalQueue struct {
	pending  chan *ApprovalRequest
	requests map[string]*ApprovalRequest
	mu       sync.Mutex
}

// NewApprovalQueue creates a new approval queue.
func NewApprovalQueue() *ApprovalQueue {
	return &ApprovalQueue{
		pending:  make(chan *ApprovalRequest, 100),
		requests: make(map[string]*ApprovalRequest),
	}
}

// Submit adds an approval request to the queue and blocks until approved or rejected.
// Returns true if approved, false if rejected.
func (q *ApprovalQueue) Submit(req *ApprovalRequest) bool {
	req.Response = make(chan bool, 1)
	req.Timestamp = time.Now()

	q.mu.Lock()
	q.requests[req.ID] = req
	q.mu.Unlock()

	q.pending <- req

	approved := <-req.Response

	q.mu.Lock()
	delete(q.requests, req.ID)
	q.mu.Unlock()

	return approved
}

// Pending returns a channel that receives approval requests as they arrive.
func (q *ApprovalQueue) Pending() <-chan *ApprovalRequest {
	return q.pending
}

// Approve approves the request with the given ID.
func (q *ApprovalQueue) Approve(id string) {
	q.mu.Lock()
	req, ok := q.requests[id]
	q.mu.Unlock()

	if ok {
		req.Response <- true
	}
}

// Reject rejects the request with the given ID.
func (q *ApprovalQueue) Reject(id string) {
	q.mu.Lock()
	req, ok := q.requests[id]
	q.mu.Unlock()

	if ok {
		req.Response <- false
	}
}
