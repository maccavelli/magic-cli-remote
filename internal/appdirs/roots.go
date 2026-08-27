package appdirs

import "path/filepath"

// Roots holds absolute base directories used to build Paths.
// Tests inject Roots; production uses SystemRoots.
type Roots struct {
	Home        string
	ConfigHome  string // XDG_CONFIG_HOME value (not product leaf)
	DataHome    string
	StateHome   string
	CacheHome   string
	RuntimeHome string // XDG_RUNTIME_DIR or validated fallback parent for product
	Temp        string
	Logs        string // service stdio base; empty where the platform has none

	// ProductScoped reports that StateHome, CacheHome and RuntimeHome are
	// already product-specific directories, so Resolve must not append the
	// product leaf again (MADR 0116 D3).
	//
	// Windows Known Folders are scoped this way (%LocalAppData%\<product>\State);
	// XDG roots are not (~/.local/state). ConfigHome and DataHome are never
	// product-scoped on either platform and always take the leaf.
	ProductScoped bool
}

// joinProduct appends the product leaf unless these roots are already
// product-scoped.
func (r Roots) joinProduct(root, name string) string {
	clean := filepath.Clean(root)
	if r.ProductScoped {
		return clean
	}
	return filepath.Join(clean, name)
}

// Diagnostic is a non-fatal path resolution note.
type Diagnostic struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// SystemRoots discovers the platform's base directories for product.
//
// Resolution is per-platform (MADR 0116 D3): Unix follows the XDG Base
// Directory specification; Windows uses Known Folders. Both feed the same
// [Resolve], which stays pure and OS-agnostic, so a test can inject either
// layout on any host.
func SystemRoots(product Product) (Roots, []Diagnostic, error) {
	return systemRoots(product)
}
