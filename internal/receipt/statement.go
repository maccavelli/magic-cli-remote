package receipt

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"
)

// StatementType is the fixed in-toto-style envelope type this package emits
// (MADR 0077 D5). New receipt kinds are new PredicateType values inside this
// same Statement shape, not a new top-level type.
const StatementType = "https://mcremote.dev/attestations/receipt/v1"

// PredicateTypePermissionDecision is the predicate type for a device-signed
// record of a human's permission decision (MADR 0077 §5.1).
const PredicateTypePermissionDecision = "https://mcremote.dev/attestations/permission-decision/v1"

// PredicateTypeReceiptUnavailable is the predicate type for a daemon-signed
// marker recorded in place of a real receipt when the device did not sign
// one in time or its signature failed to verify (MADR 0077 D8).
const PredicateTypeReceiptUnavailable = "https://mcremote.dev/attestations/receipt-unavailable/v1"

// Statement is the signed JWS payload for every receipt kind this package
// produces (MADR 0077 D5, an in-toto Attestation Framework-style envelope).
// PredicateType is the extension point: a future receipt kind is a new
// PredicateType and a new predicate payload struct, not a new envelope
// shape — see docs/receipts.md's predicateType registry.
type Statement struct {
	Type          string               `json:"_type"`
	Subject       []ResourceDescriptor `json:"subject"`
	PredicateType string               `json:"predicateType"`
	Predicate     json.RawMessage      `json:"predicate"`
	Chain         ChainLink            `json:"chain"`
}

// ResourceDescriptor identifies the thing this Statement attests to,
// in-toto's Subject shape: a name and one or more digests.
type ResourceDescriptor struct {
	Name   string            `json:"name"`
	Digest map[string]string `json:"digest"`
}

// ChainLink is the backward hash chain a Statement's line participates in
// (MADR 0077 D6), carried at the top level — outside the type-specific
// Predicate — so every receipt kind, present or future, chains the same way.
type ChainLink struct {
	// Scope identifies the chain this entry belongs to: "device:<deviceID>"
	// for every entry currently produced (D6: chained per device, not per
	// session) — both permission-decision and receipt-unavailable entries
	// for the same device share one chain.
	Scope string `json:"scope"`
	// PrevSHA256 is the SHA-256 (lowercase hex) of the complete previous
	// stored line for this Scope, or nil for the first entry in the chain.
	PrevSHA256 *string `json:"prev_sha256"`
}

// PermissionDecisionPredicate is PredicateTypePermissionDecision's payload:
// what a human decided, when, about which tool call, and who decided it.
type PermissionDecisionPredicate struct {
	DeviceID  string    `json:"device_id"`
	OptionID  string    `json:"option_id"`
	DecidedAt time.Time `json:"decided_at"`
	ToolName  string    `json:"tool_name"`
	Detail    string    `json:"detail"`
}

// UnavailablePredicate is PredicateTypeReceiptUnavailable's payload:
// recorded, daemon-signed, when a device-signed receipt could not be
// obtained (MADR 0077 D8).
type UnavailablePredicate struct {
	// Reason is "timeout" (the device did not reply within D8's 10-second
	// window) or "invalid_signature" (it replied, but the signature did not
	// verify against the device's persisted public key).
	Reason       string `json:"reason"`
	PermissionID string `json:"permission_id"`
	DeviceID     string `json:"device_id"`
}

// subjectDigest hashes the exact tool-call content this Statement attests
// to, binding the receipt to the real action (MADR 0077 §2 point 2) rather
// than trusting free text to stay in sync with what the provider reported.
func subjectDigest(toolName, detail string) string {
	sum := sha256.Sum256([]byte(toolName + "\x00" + detail))
	return hex.EncodeToString(sum[:])
}

// BuildPermissionDecisionStatement builds the Statement for a resolved
// permission decision, ready to be marshaled and signed (P7). chainScope is
// "device:<deviceID>" (D6); prevSHA256 is nil for a device's first-ever
// entry.
func BuildPermissionDecisionStatement(sessionID, permissionID, deviceID, optionID, toolName, detail string, decidedAt time.Time, chainScope string, prevSHA256 *string) (*Statement, error) {
	predicate, err := json.Marshal(PermissionDecisionPredicate{
		DeviceID:  deviceID,
		OptionID:  optionID,
		DecidedAt: decidedAt,
		ToolName:  toolName,
		Detail:    detail,
	})
	if err != nil {
		return nil, fmt.Errorf("marshal permission-decision predicate: %w", err)
	}
	return &Statement{
		Type: StatementType,
		Subject: []ResourceDescriptor{{
			Name:   "session:" + sessionID + "/permission:" + permissionID,
			Digest: map[string]string{"sha256": subjectDigest(toolName, detail)},
		}},
		PredicateType: PredicateTypePermissionDecision,
		Predicate:     predicate,
		Chain:         ChainLink{Scope: chainScope, PrevSHA256: prevSHA256},
	}, nil
}

// BuildReceiptUnavailableStatement builds the marker Statement recorded when
// no valid device-signed receipt could be obtained (MADR 0077 D8). Its
// chain.scope is still the device's own chain — a failed-to-sign entry
// belongs in that device's sequence, so the backward walk stays intact.
func BuildReceiptUnavailableStatement(permissionID, deviceID, reason, chainScope string, prevSHA256 *string) (*Statement, error) {
	predicate, err := json.Marshal(UnavailablePredicate{
		Reason:       reason,
		PermissionID: permissionID,
		DeviceID:     deviceID,
	})
	if err != nil {
		return nil, fmt.Errorf("marshal receipt-unavailable predicate: %w", err)
	}
	sum := sha256.Sum256([]byte(permissionID + "\x00" + deviceID + "\x00" + reason))
	return &Statement{
		Type: StatementType,
		Subject: []ResourceDescriptor{{
			Name:   "permission:" + permissionID,
			Digest: map[string]string{"sha256": hex.EncodeToString(sum[:])},
		}},
		PredicateType: PredicateTypeReceiptUnavailable,
		Predicate:     predicate,
		Chain:         ChainLink{Scope: chainScope, PrevSHA256: prevSHA256},
	}, nil
}
