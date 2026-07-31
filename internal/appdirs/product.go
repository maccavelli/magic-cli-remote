// Package appdirs resolves XDG Base Directory paths for mcremote and mcrelay
// on Linux and Darwin (MADR 0059). Resolution is pure; Ensure creates only
// what callers request.
package appdirs

// Product identifies a binary and its XDG leaf / launchd label.
type Product struct {
	// Name is the XDG product leaf (e.g. "mcremote").
	Name string
	// LaunchLabel is the reverse-DNS launchd label (service only).
	LaunchLabel string
}

// Known products.
var (
	ProductMcremote = Product{
		Name:        "mcremote",
		LaunchLabel: "com.magiccliremote.mcremote",
	}
	ProductMcrelay = Product{
		Name:        "mcrelay",
		LaunchLabel: "com.magiccliremote.mcrelay",
	}
)

// ProductByName returns a known product or an empty Product with ok false.
func ProductByName(name string) (Product, bool) {
	switch name {
	case ProductMcremote.Name:
		return ProductMcremote, true
	case ProductMcrelay.Name:
		return ProductMcrelay, true
	default:
		return Product{}, false
	}
}

// Valid reports whether p has a non-empty Name.
func (p Product) Valid() bool {
	return p.Name != ""
}
