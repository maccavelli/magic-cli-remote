package auth

import "time"

// lockTimeout bounds how long an operation waits for the cross-process lock.
// Legitimate holders (a small devices.json rewrite) release in milliseconds; a
// hold this long means a peer process wedged while holding the lock. Blocking
// forever there would stall ALL authentication (Validate takes this lock), so a
// bounded failure the caller can retry is strictly better than an indefinite hang.
const lockTimeout = 5 * time.Second
