package services

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	swarmtypes "github.com/getarcaneapp/arcane/types/v2/swarm"
)

func TestNodeAgentStatusLegacyJSONCompatibility(t *testing.T) {
	var status swarmtypes.NodeAgentStatus
	if err := json.Unmarshal([]byte(`{"state":"connected","environmentId":"env-1","connected":true}`), &status); err != nil {
		t.Fatalf("unmarshal legacy status: %v", err)
	}
	if status.State != swarmtypes.NodeAgentStateConnected {
		t.Fatalf("state = %q, want %q", status.State, swarmtypes.NodeAgentStateConnected)
	}
	if status.EnvironmentID == nil || *status.EnvironmentID != "env-1" {
		t.Fatalf("environmentId = %v, want env-1", status.EnvironmentID)
	}
	if status.BindingKind != nil {
		t.Fatalf("bindingKind = %v, want nil", *status.BindingKind)
	}
	if len(status.Candidates) != 0 {
		t.Fatalf("candidates = %v, want empty", status.Candidates)
	}
}

func TestNodeAgentStatusAmbiguousJSON(t *testing.T) {
	status := swarmtypes.NodeAgentStatus{
		State: swarmtypes.NodeAgentStateAmbiguous,
		Candidates: []swarmtypes.NodeAgentCandidate{{
			EnvironmentID:   "env-1",
			EnvironmentName: "worker-1",
			EnvironmentType: "edge",
		}},
	}

	encoded, err := json.Marshal(status)
	if err != nil {
		t.Fatalf("marshal ambiguous status: %v", err)
	}
	var actual, expected any
	if err := json.Unmarshal(encoded, &actual); err != nil {
		t.Fatalf("unmarshal actual JSON: %v", err)
	}
	if err := json.Unmarshal([]byte(`{"state":"ambiguous","candidates":[{"environmentId":"env-1","environmentName":"worker-1","environmentType":"edge"}]}`), &expected); err != nil {
		t.Fatalf("unmarshal expected JSON: %v", err)
	}
	if !reflect.DeepEqual(actual, expected) {
		t.Fatalf("JSON = %s, want ambiguous candidate payload", encoded)
	}
}

func TestSwarmJoinEnvironmentResultDoesNotExposeToken(t *testing.T) {
	result := swarmtypes.SwarmJoinEnvironmentResult{EnvironmentID: "env-1", State: swarmtypes.SwarmJoinEnvironmentResultJoined}
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal join result: %v", err)
	}
	if string(encoded) == "" || strings.Contains(string(encoded), "token") {
		t.Fatalf("join result unexpectedly exposes a token field: %s", encoded)
	}
}
