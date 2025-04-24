package coordination

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/mux"
)

// Response struct for standardized API responses
type APIResponse struct {
	Success bool        `json:"success"`
	Error   string      `json:"error,omitempty"`
	Data    interface{} `json:"data,omitempty"`
}

// RequestWithID is a common struct for requests that contain an ID
type RegisterWorkerRequest struct {
	ID        string `json:"id,omitempty"`
	WorkerID  string `json:"worker_id,omitempty"`
	EnclaveID []byte `json:"enclave_id"`
}

// TEEPairRequest is used for registering TEE pairs
type TEEPairRequest struct {
	PrimaryWorkerID   string   `json:"primary_worker_id"`
	SecondaryWorkerID string   `json:"secondary_worker_id"`
	Attestations      [][]byte `json:"attestations,omitempty"`
}

// TaskSubmitRequest is used for submitting tasks
type TaskSubmitRequest struct {
	ID          string   `json:"id"`
	WorkerIDs   []string `json:"worker_ids"`
	Data        []byte   `json:"data"`
	Attestations [][]byte `json:"attestations"`
	Timeout     uint64   `json:"timeout"`
	RegionID    string   `json:"region_id"`
}

// TaskResultRequest is used for submitting task results
type TaskResultRequest struct {
	Result   []byte `json:"result"`
	WorkerID string `json:"worker_id"`
}

// WorkerResponse represents a worker in responses
type WorkerResponse struct {
	ID        string `json:"id"`
	EnclaveID []int  `json:"enclave_id"`
	Status    uint8  `json:"status"`
}

// CoordinatorServer implements an HTTP server for the coordinator
type CoordinatorServer struct {
	coordinator *Coordinator
	router      *mux.Router
	server      *http.Server
}

// NewCoordinatorServer creates a new HTTP API server for a coordinator
func NewCoordinatorServer(coordinator *Coordinator, listenAddr string) *CoordinatorServer {
	router := mux.NewRouter()
	server := &http.Server{
		Addr:    listenAddr,
		Handler: router,
	}

	cs := &CoordinatorServer{
		coordinator: coordinator,
		router:      router,
		server:      server,
	}

	// Register routes
	cs.registerRoutes()

	return cs
}

// registerRoutes sets up all the HTTP routes for the coordinator API
func (cs *CoordinatorServer) registerRoutes() {
	// Worker management
	cs.router.HandleFunc("/workers", cs.handleListWorkers).Methods("GET")
	cs.router.HandleFunc("/workers", cs.handleRegisterWorker).Methods("POST")
	cs.router.HandleFunc("/workers/register", cs.handleRegisterWorker).Methods("POST")
	cs.router.HandleFunc("/workers/{worker_id}", cs.handleGetWorker).Methods("GET")
	cs.router.HandleFunc("/workers/{worker_id}", cs.handleUnregisterWorker).Methods("DELETE")
	cs.router.HandleFunc("/workers/{worker_id}/tee_pairs", cs.handleRegisterTEEPair).Methods("POST")
	cs.router.HandleFunc("/workers/region/{region_id}", cs.handleGetRegionWorkers).Methods("GET")

	// Region management
	cs.router.HandleFunc("/regions", cs.handleListRegions).Methods("GET")
	cs.router.HandleFunc("/regions/{region_id}/pairs/register", cs.handleRegisterTEEPairWithTimeout).Methods("POST")

	// Task management
	cs.router.HandleFunc("/tasks", cs.handleSubmitTask).Methods("POST")
	cs.router.HandleFunc("/tasks/submit", cs.handleSubmitTask).Methods("POST")
	cs.router.HandleFunc("/tasks/{task_id}", cs.handleGetTaskStatus).Methods("GET")
	cs.router.HandleFunc("/tasks/{task_id}/status", cs.handleGetTaskStatus).Methods("GET")
	cs.router.HandleFunc("/tasks/{task_id}/result", cs.handleSetTaskResult).Methods("POST")

	// Health check
	cs.router.HandleFunc("/health", cs.handleHealthCheck).Methods("GET")
}

// Start starts the HTTP server
func (cs *CoordinatorServer) Start() error {
	log.Printf("Starting coordinator HTTP server on %s", cs.server.Addr)
	return cs.server.ListenAndServe()
}

// Stop gracefully stops the HTTP server
func (cs *CoordinatorServer) Stop(ctx context.Context) error {
	return cs.server.Shutdown(ctx)
}

// writeError is a helper function to write error responses
func writeError(w http.ResponseWriter, status int, err error) {
	resp := APIResponse{
		Success: false,
		Error:   err.Error(),
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(resp)
}

// writeSuccess is a helper function to write success responses
func writeSuccess(w http.ResponseWriter, data interface{}) {
	resp := APIResponse{
		Success: true,
		Data:    data,
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(resp)
}

// Route handlers

// handleHealthCheck responds to health check requests
func (cs *CoordinatorServer) handleHealthCheck(w http.ResponseWriter, r *http.Request) {
	writeSuccess(w, map[string]interface{}{
		"status": "ok",
	})
}

// handleRegisterTEEPairWithTimeout registers a TEE pair for a specific region with proper timeout handling
func (cs *CoordinatorServer) handleRegisterTEEPairWithTimeout(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	regionID := vars["region_id"]
	log.Printf("Received TEE pair registration request for region: %s", regionID)
	
	// Read the entire request body for logging
	requestBody, err := io.ReadAll(r.Body)
	if err != nil {
		log.Printf("Error reading request body: %v", err)
		writeError(w, http.StatusBadRequest, fmt.Errorf("failed to read request body: %v", err))
		return
	}
	
	// Log the raw request for debugging
	log.Printf("Raw TEE pair registration request: %s", string(requestBody))
	
	// Re-create a new reader with the content we just read
	r.Body = io.NopCloser(bytes.NewBuffer(requestBody))
	
	// Decode the request
	var req TEEPairRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Printf("Failed to decode TEE pair request: %v", err)
		writeError(w, http.StatusBadRequest, fmt.Errorf("invalid request format: %v", err))
		return
	}
	
	log.Printf("Decoded TEE pair request: primary=%s, secondary=%s", req.PrimaryWorkerID, req.SecondaryWorkerID)
	
	if req.PrimaryWorkerID == "" {
		writeError(w, http.StatusBadRequest, fmt.Errorf("primary worker ID is required"))
		return
	}
	
	if req.SecondaryWorkerID == "" {
		writeError(w, http.StatusBadRequest, fmt.Errorf("secondary worker ID is required"))
		return
	}
	
	// Create TEE pair
	teePair := &TEEPair{
		SGXID: []byte(req.PrimaryWorkerID),
		SEVID: []byte(req.SecondaryWorkerID),
	}
	
	// Create a context with a reasonable timeout
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	
	// Use a goroutine with a channel to handle the registration process with a timeout
	errCh := make(chan error, 1)
	go func() {
		errCh <- cs.coordinator.RegisterTEEPair(ctx, regionID, teePair)
	}()
	
	// Wait for either completion or timeout
	select {
	case regErr := <-errCh:
		if regErr != nil {
			log.Printf("Error registering TEE pair: %v", regErr)
			writeError(w, http.StatusInternalServerError, regErr)
			return
		}
		log.Printf("Successfully registered TEE pair")
	case <-ctx.Done():
		log.Printf("TEE pair registration timed out")
		writeError(w, http.StatusRequestTimeout, fmt.Errorf("TEE pair registration timed out: %w", ctx.Err()))
		return
	}
	
	// If we get here, the operation was successful
	writeSuccess(w, map[string]interface{}{
		"tee_pair": map[string]string{
			"primary_worker_id":   req.PrimaryWorkerID,
			"secondary_worker_id": req.SecondaryWorkerID,
			"region_id":           regionID,
		},
	})
}

// handleRegisterWorker handles worker registration
func (cs *CoordinatorServer) handleRegisterWorker(w http.ResponseWriter, r *http.Request) {
	var req RegisterWorkerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Errorf("invalid request format: %v", err))
		return
	}

	// Handle both id and worker_id fields for flexibility
	workerID := req.WorkerID
	if workerID == "" {
		workerID = req.ID
	}

	if workerID == "" {
		writeError(w, http.StatusBadRequest, fmt.Errorf("worker ID is required"))
		return
	}

	if len(req.EnclaveID) == 0 {
		writeError(w, http.StatusBadRequest, fmt.Errorf("enclave ID is required"))
		return
	}

	ctx := r.Context()
	err := cs.coordinator.RegisterWorker(ctx, WorkerID(workerID), req.EnclaveID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	writeSuccess(w, map[string]string{"worker_id": workerID})
}

// handleListWorkers lists all registered workers
func (cs *CoordinatorServer) handleListWorkers(w http.ResponseWriter, r *http.Request) {
	workerIDs := cs.coordinator.GetWorkerIDs()
	workers := make([]WorkerResponse, 0, len(workerIDs))

	for _, id := range workerIDs {
		worker, exists := cs.coordinator.GetWorker(id)
		if exists {
			// Convert byte slice to int slice for JSON compatibility with Rust client
			enclaveInts := make([]int, len(worker.EnclaveID))
			for i, b := range worker.EnclaveID {
				enclaveInts[i] = int(b)
			}
			
			workers = append(workers, WorkerResponse{
				ID:        string(worker.ID),
				EnclaveID: enclaveInts,
				Status:    uint8(worker.Status),
			})
		}
	}

	writeSuccess(w, workers)
}

// handleGetWorker gets information about a specific worker
func (cs *CoordinatorServer) handleGetWorker(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	workerID := vars["worker_id"]

	worker, exists := cs.coordinator.GetWorker(WorkerID(workerID))
	if !exists {
		writeError(w, http.StatusNotFound, fmt.Errorf("worker not found"))
		return
	}

	// Convert byte slice to int slice for JSON compatibility with Rust client
	enclaveInts := make([]int, len(worker.EnclaveID))
	for i, b := range worker.EnclaveID {
		enclaveInts[i] = int(b)
	}
	
	resp := WorkerResponse{
		ID:        string(worker.ID),
		EnclaveID: enclaveInts,
		Status:    uint8(worker.Status),
	}

	writeSuccess(w, resp)
}

// handleUnregisterWorker unregisters a worker
func (cs *CoordinatorServer) handleUnregisterWorker(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	workerID := vars["worker_id"]

	ctx := r.Context()
	err := cs.coordinator.UnregisterWorker(ctx, WorkerID(workerID))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	writeSuccess(w, map[string]bool{"unregistered": true})
}

// handleRegisterTEEPair registers a TEE pair
func (cs *CoordinatorServer) handleRegisterTEEPair(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	workerID := vars["worker_id"]

	var req TEEPairRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Errorf("invalid request format: %v", err))
		return
	}

	// If the primary worker ID is not specified in the request, use the one from the URL
	if req.PrimaryWorkerID == "" {
		req.PrimaryWorkerID = workerID
	}

	// Consistency check
	if req.PrimaryWorkerID != workerID {
		writeError(w, http.StatusBadRequest, fmt.Errorf("primary worker ID mismatch"))
		return
	}

	if req.SecondaryWorkerID == "" {
		writeError(w, http.StatusBadRequest, fmt.Errorf("secondary worker ID is required"))
		return
	}

	// Create TEE pair
	teePair := &TEEPair{
		SGXID: []byte(req.PrimaryWorkerID),
		SEVID: []byte(req.SecondaryWorkerID),
	}

	// Register the pair (use 'default' region if not specified)
	regionID := "default"

	ctx := r.Context()
	err := cs.coordinator.RegisterTEEPair(ctx, regionID, teePair)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	writeSuccess(w, map[string]interface{}{
		"tee_pair": map[string]string{
			"primary_worker_id":   req.PrimaryWorkerID,
			"secondary_worker_id": req.SecondaryWorkerID,
			"region_id":           regionID,
		},
	})
}

// handleRegisterTEEPairByRegion registers a TEE pair for a specific region
func (cs *CoordinatorServer) handleRegisterTEEPairByRegion(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	regionID := vars["region_id"]
	log.Printf("Received TEE pair registration request for region: %s", regionID)

	// Read the entire request body for logging
	requestBody, err := io.ReadAll(r.Body)
	if err != nil {
		log.Printf("Error reading request body: %v", err)
		writeError(w, http.StatusBadRequest, fmt.Errorf("failed to read request body: %v", err))
		return
	}

	// Log the raw request
	log.Printf("Raw TEE pair registration request: %s", string(requestBody))

	// Re-create a new reader with the content we just read
	r.Body = io.NopCloser(bytes.NewBuffer(requestBody))

	// Decode the request
	var req TEEPairRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Printf("Failed to decode TEE pair request: %v", err)
		writeError(w, http.StatusBadRequest, fmt.Errorf("invalid request format: %v", err))
		return
	}

	log.Printf("Decoded TEE pair request: primary=%s, secondary=%s", req.PrimaryWorkerID, req.SecondaryWorkerID)

	if req.PrimaryWorkerID == "" {
		writeError(w, http.StatusBadRequest, fmt.Errorf("primary worker ID is required"))
		return
	}

	if req.SecondaryWorkerID == "" {
		writeError(w, http.StatusBadRequest, fmt.Errorf("secondary worker ID is required"))
		return
	}

	// Create TEE pair
	teePair := &TEEPair{
		SGXID: []byte(req.PrimaryWorkerID),
		SEVID: []byte(req.SecondaryWorkerID),
	}

	ctx := r.Context()
	err = cs.coordinator.RegisterTEEPair(ctx, regionID, teePair)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	writeSuccess(w, map[string]interface{}{
		"tee_pair": map[string]string{
			"primary_worker_id":   req.PrimaryWorkerID,
			"secondary_worker_id": req.SecondaryWorkerID,
			"region_id":           regionID,
		},
	})
}

// handleGetRegionWorkers gets workers in a specific region
func (cs *CoordinatorServer) handleGetRegionWorkers(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	regionID := vars["region_id"]

	// This is a placeholder - we need to implement GetWorkersInRegion in the coordinator
	// For now, we'll just return all workers with a note that they're in the region
	workerIDs := cs.coordinator.GetWorkerIDs()
	workers := make([]WorkerResponse, 0, len(workerIDs))

	for _, id := range workerIDs {
		worker, exists := cs.coordinator.GetWorker(id)
		if exists {
			// Convert byte slice to int slice for JSON compatibility with Rust client
			enclaveInts := make([]int, len(worker.EnclaveID))
			for i, b := range worker.EnclaveID {
				enclaveInts[i] = int(b)
			}
			
			workers = append(workers, WorkerResponse{
				ID:        string(worker.ID),
				EnclaveID: enclaveInts,
				Status:    uint8(worker.Status),
			})
		}
	}

	writeSuccess(w, map[string]interface{}{
		"region_id": regionID,
		"workers":   workers,
	})
}

// handleListRegions lists all regions
func (cs *CoordinatorServer) handleListRegions(w http.ResponseWriter, r *http.Request) {
	// This is a placeholder - we need to implement GetRegions in the coordinator
	// For now, we'll return a default region
	regions := []map[string]interface{}{
		{
			"id":      "default",
			"status":  "active",
			"workers": 0,
		},
	}

	writeSuccess(w, map[string]interface{}{"regions": regions})
}

// handleSubmitTask submits a task
func (cs *CoordinatorServer) handleSubmitTask(w http.ResponseWriter, r *http.Request) {
	var req TaskSubmitRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Errorf("invalid request format: %v", err))
		return
	}

	// Create the task
	task := &Task{
		ID:           req.ID,
		Data:         req.Data,
		Attestations: req.Attestations,
		Timeout:      time.Duration(req.Timeout) * time.Millisecond,
	}

	// Convert worker IDs
	for _, id := range req.WorkerIDs {
		task.WorkerIDs = append(task.WorkerIDs, WorkerID(id))
	}

	// Default region if not specified
	regionID := req.RegionID
	if regionID == "" {
		regionID = "default"
	}

	ctx := r.Context()
	err := cs.coordinator.SubmitTask(ctx, task)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	writeSuccess(w, map[string]string{"task_id": task.ID})
}

// handleGetTaskStatus gets the status of a task
func (cs *CoordinatorServer) handleGetTaskStatus(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	taskID := vars["task_id"]

	taskInfo, err := cs.coordinator.GetTaskStatus(taskID)
	if err != nil {
		status := http.StatusInternalServerError
		if strings.Contains(err.Error(), "not found") {
			status = http.StatusNotFound
		}
		writeError(w, status, err)
		return
	}

	// Format response
	response := map[string]interface{}{
		"task": map[string]interface{}{
			"id":           taskInfo.Task.ID,
			"worker_ids":   taskInfo.Task.WorkerIDs,
			"data":         taskInfo.Task.Data,
			"attestations": taskInfo.Task.Attestations,
			"timeout":      taskInfo.Task.Timeout.Milliseconds(),
		},
		"status":     string(taskInfo.Status),
		"start_time": taskInfo.StartTime.Format(time.RFC3339),
		"end_time":   taskInfo.EndTime.Format(time.RFC3339),
		"results":    taskInfo.Results,
	}

	if taskInfo.Error != nil {
		response["error"] = taskInfo.Error.Error()
	}

	writeSuccess(w, response)
}

// handleSetTaskResult sets the result for a task
func (cs *CoordinatorServer) handleSetTaskResult(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	taskID := vars["task_id"]

	var req TaskResultRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Errorf("invalid request format: %v", err))
		return
	}

	err := cs.coordinator.SetTaskResult(taskID, req.Result, WorkerID(req.WorkerID))
	if err != nil {
		status := http.StatusInternalServerError
		if strings.Contains(err.Error(), "not found") {
			status = http.StatusNotFound
		}
		writeError(w, status, err)
		return
	}

	writeSuccess(w, map[string]bool{"success": true})
}
