package grok

import "testing"

// T-C1: both 1.0.5 slash and 0039 underscore names must be registered
// (MADR 0081 P1.2).
func TestSpecRegistersBothModelsUpdateNames(t *testing.T) {
	slash := spec.ExtensionNotifications["_x.ai/models/update"]
	underscore := spec.ExtensionNotifications["_x.ai/models_update"]
	if slash == nil || underscore == nil {
		t.Fatalf("models-update handlers slash=%v underscore=%v", slash != nil, underscore != nil)
	}
}
