// tee/types.go
package tee

import (
    "fmt"
    "time"

    "github.com/rhombus-tech/vm/tee/proto"
    "github.com/rhombus-tech/vm/core"
)

const (
    TEETypeSGX = "SGX"
    TEETypeSEV = "SEV"
    TEETypeTDX = "TDX"
)

// TEEPairInfo represents metadata about a TEE pair
type TEEPairInfo struct {
    ID          string `json:"id"`
    SGXEndpoint string `json:"sgx_endpoint"`
    SEVEndpoint string `json:"sev_endpoint"`
    TDXEndpoint string `json:"tdx_endpoint"`
    Status      string `json:"status"`
    Attestations [3]core.TEEAttestation `json:"attestations,omitempty"`
}

// TEEPairConfig defines configuration for a TEE pair
type TEEPairConfig struct {
    ID          string        `json:"id"`
    SGXEndpoint string        `json:"sgx_endpoint"`
    SEVEndpoint string        `json:"sev_endpoint"`
    TDXEndpoint string        `json:"tdx_endpoint"`
    Thresholds  Thresholds    `json:"thresholds"`
}

// Thresholds defines operational thresholds for a TEE pair
type Thresholds struct {
    MinSuccessRate float64       `json:"min_success_rate"`
    MaxErrorRate   float64       `json:"max_error_rate"`
    MaxLatency     time.Duration `json:"max_latency"`
    MaxLoadFactor  float64       `json:"max_load_factor"`
}

// TEEPairMetrics holds runtime metrics for a TEE pair
type TEEPairMetrics struct {
    PairID          string        `json:"pair_id"`
    SGXEndpoint     string        `json:"sgx_endpoint"`
    SEVEndpoint     string        `json:"sev_endpoint"`
    TDXEndpoint     string        `json:"tdx_endpoint"`
    LastHealthCheck time.Time     `json:"last_health_check"`
    LastHealthy     time.Time     `json:"last_healthy"`
    SuccessRate     float64       `json:"success_rate"`
    LoadFactor      float64       `json:"load_factor"`
    ExecutionTime   time.Duration `json:"execution_time"`
    ConsecutiveErrors uint64      `json:"consecutive_errors"`
}

// ShuttleEvent represents an event emitted from the shuttle system
type ShuttleEvent struct {
    ID           string                 `json:"id"`
    FunctionCall string                 `json:"functionCall"`
    Parameters   []byte                 `json:"parameters"`
    RegionID     string                 `json:"regionId"`
    Timestamp    time.Time              `json:"timestamp"`
    Attestations []*core.TEEAttestation `json:"attestations"`
}

// Validate checks if a ShuttleEvent is valid
func (e *ShuttleEvent) Validate() error {
    if e.ID == "" {
        return fmt.Errorf("empty event ID")
    }
    if e.FunctionCall == "" {
        return fmt.Errorf("empty function call")
    }
    if len(e.Attestations) == 0 {
        return fmt.Errorf("no attestations")
    }
    return nil
}

// ToProto converts a ShuttleEvent to a proto message
func (e *ShuttleEvent) ToProto() *proto.Event {
    protoAtts := make([]*proto.TEEAttestation, len(e.Attestations))
    for i, att := range e.Attestations {
        protoAtts[i] = &proto.TEEAttestation{
            EnclaveId:   att.EnclaveID,
            Measurement: att.Measurement,
            Timestamp:   e.Timestamp.Format(time.RFC3339),
        }
    }
    
    return &proto.Event{
        Id:           e.ID,
        FunctionCall: e.FunctionCall,
        Parameters:   e.Parameters,
        RegionId:     e.RegionID,
        Timestamp:    e.Timestamp.Format(time.RFC3339),
        Attestations: protoAtts,
    }
}

// FromProto converts a proto message to a ShuttleEvent
func ShuttleEventFromProto(event *proto.Event) (*ShuttleEvent, error) {
    if event == nil {
        return nil, fmt.Errorf("nil event")
    }
    
    // Parse timestamp
    var timestamp time.Time
    var err error
    if event.Timestamp != "" {
        timestamp, err = time.Parse(time.RFC3339, event.Timestamp)
        if err != nil {
            return nil, fmt.Errorf("failed to parse timestamp: %w", err)
        }
    } else {
        timestamp = time.Now()
    }
    
    // Convert attestations
    atts, err := convertProtoAttestations(event.Attestations)
    if err != nil {
        return nil, fmt.Errorf("failed to convert attestations: %w", err)
    }
    
    return &ShuttleEvent{
        ID:           event.Id,
        FunctionCall: event.FunctionCall,
        Parameters:   event.Parameters,
        RegionID:     event.RegionId,
        Timestamp:    timestamp,
        Attestations: atts,
    }, nil
}

// Convert between proto and core types
func toProtoAttestation(att core.TEEAttestation) *proto.TEEAttestation {
    return &proto.TEEAttestation{
        EnclaveId:   att.EnclaveID,
        Measurement: att.Measurement,
        Timestamp:   att.Timestamp.Format(time.RFC3339),
        Data:        att.Data,
        Signature:   att.Signature,
        RegionProof: att.RegionProof,
    }
}

func fromProtoAttestation(att *proto.TEEAttestation) (core.TEEAttestation, error) {
    timestamp, err := time.Parse(time.RFC3339, att.Timestamp)
    if err != nil {
        return core.TEEAttestation{}, fmt.Errorf("invalid timestamp format: %w", err)
    }

    return core.TEEAttestation{
        EnclaveID:   att.EnclaveId,
        Measurement: att.Measurement,
        Timestamp:   timestamp,
        Data:        att.Data,
        Signature:   att.Signature,
        RegionProof: att.RegionProof,
    }, nil
}

// Convert protocol buffer attestation list to internal format
func convertProtoAttestations(attestations []*proto.TEEAttestation) ([]*core.TEEAttestation, error) {
    if len(attestations) == 0 {
        return nil, fmt.Errorf("no attestations provided")
    }
    
    result := make([]*core.TEEAttestation, len(attestations))
    for i, att := range attestations {
        converted, err := protoToCoreAttestation(att)
        if err != nil {
            return nil, fmt.Errorf("failed to convert attestation %d: %w", i, err)
        }
        result[i] = &converted
    }
    
    return result, nil
}

// Convert from proto TEEAttestation to core TEEAttestation
func protoToCoreAttestation(proto *proto.TEEAttestation) (core.TEEAttestation, error) {
    timestamp, err := time.Parse(time.RFC3339, proto.Timestamp)
    if err != nil {
        return core.TEEAttestation{}, err
    }
    
    return core.TEEAttestation{
        EnclaveID:   proto.EnclaveId,
        Measurement: proto.Measurement,
        Timestamp:   timestamp,
        Data:        proto.Data,
        Signature:   proto.Signature,
        RegionProof: proto.RegionProof,
    }, nil
}

// protoToCoreAttestations converts a slice of proto attestations to a slice of core attestations
func protoToCoreAttestations(protos []*proto.TEEAttestation) ([]*core.TEEAttestation, error) {
    if len(protos) == 0 {
        return nil, fmt.Errorf("no attestations provided")
    }
    
    result := make([]*core.TEEAttestation, len(protos))
    for i, proto := range protos {
        converted, err := protoToCoreAttestation(proto)
        if err != nil {
            return nil, fmt.Errorf("failed to convert attestation %d: %w", i, err)
        }
        result[i] = &converted
    }
    
    return result, nil
}