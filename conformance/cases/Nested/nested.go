package nested

type Item struct {
	Sku string `json:"sku"`
	Qty int64  `json:"qty"`
}

type Nested struct {
	Items      []Item            `json:"items"`
	Attributes map[string]string `json:"attributes"`
}
