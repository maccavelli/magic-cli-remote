package certs

import "time"

// certLockTimeout bounds waiting for the cert-generation lock. A generate is a
// couple of small file writes; a hold this long means a peer wedged, and a
// bounded failure beats blocking daemon startup forever.
const certLockTimeout = 10 * time.Second

// certLockName is the lock file inside the cert directory, shared by every
// platform's lockCertDir so the two implementations cannot pick different
// names and stop excluding each other.
const certLockName = ".certs.lock"
