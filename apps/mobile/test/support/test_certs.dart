/// Shared TLS fixtures for tests that need a pinnable daemon certificate.
///
/// A self-signed P-256 leaf produced by `internal/certs`, plus a second,
/// unrelated fingerprint. Together they drive the exact case pinning exists
/// for: a host that answers on the right address with the wrong identity.
library;

const certA = '''
-----BEGIN CERTIFICATE-----
MIIB/DCCAaKgAwIBAgIRAN5NFABi09Yr97dp1e9iC/kwCgYIKoZIzj0EAwIwJjER
MA8GA1UEChMIbWNyZW1vdGUxETAPBgNVBAMTCG1jcmVtb3RlMB4XDTI2MDcyMDAx
MTI1M1oXDTM2MDcxNzAyMTI1M1owJjERMA8GA1UEChMIbWNyZW1vdGUxETAPBgNV
BAMTCG1jcmVtb3RlMFkwEwYHKoZIzj0CAQYIKoZIzj0DAQcDQgAEkvA76EbbgRKk
sR/inxlmOX9/3kBAU+vuDF32gJA3D5/dmwm6CQv5K4SiMtx1Ns3mvOEblMLaAKRE
pMcbKxLisqOBsDCBrTAOBgNVHQ8BAf8EBAMCAqQwEwYDVR0lBAwwCgYIKwYBBQUH
AwEwDwYDVR0TAQH/BAUwAwEB/zAdBgNVHQ4EFgQUVPhZhHKkgG0GeQXpOT31i4DU
Kn0wVgYDVR0RBE8wTYIKYXdzdXRpbGl0eYIJbG9jYWxob3N0hwQKCnothwRkQAAB
hwR/AAABhxAAAAAAAAAAAAAAAAAAAAABhxD9ehFcoeAAAAAAAAAAAAABMAoGCCqG
SM49BAMCA0gAMEUCIAqmW6YGO1OXwXgnGrZOvqkyD+ST9+VAp0Yl157YwgaeAiEA
oxcGdNiqe/vT+pSAsKl9BIbTH3UU0BSGc7dDaemKowE=
-----END CERTIFICATE-----
''';

const keyA = '''
-----BEGIN EC PRIVATE KEY-----
MHcCAQEEIGB3oZEd8f81ajOahNu3oYN6u7dw1ll6LdUNYeNsXr9poAoGCCqGSM49
AwEHoUQDQgAEkvA76EbbgRKksR/inxlmOX9/3kBAU+vuDF32gJA3D5/dmwm6CQv5
K4SiMtx1Ns3mvOEblMLaAKREpMcbKxLisg==
-----END EC PRIVATE KEY-----
''';

/// SHA-256 of [certA]'s DER, base64url — what the daemon prints into the QR.
const fpA = 'Qe0O3GUSGnH0EzRTJC0Lo1mthKzPt_LUuymFymtngd0';

/// A different daemon's fingerprint. Never served by [certA].
const fpB = 'YaLoAn6LtTyDz3E9u74B5VFROBwLQc_xDOx-xlCH_JE';
