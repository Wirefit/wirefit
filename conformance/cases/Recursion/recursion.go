package recursion

type Recursion struct {
	Name     string      `json:"name"`
	Children []Recursion `json:"children"`
}
