package maps

type Item struct {
	Sku string `json:"sku"`
	Qty int64  `json:"qty"`
}

// Maps exercises typed map values: a scalar-valued and an object-valued map
// must produce hash-identical IR across languages, value type and all.
type Maps struct {
	Labels map[string]string `json:"labels"`
	Items  map[string]Item   `json:"items"`
}
