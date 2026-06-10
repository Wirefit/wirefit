package scalars

import "time"

type Scalars struct {
	Name    string    `json:"name"`
	Count   int64     `json:"count"`
	Price   float64   `json:"price"`
	Active  bool      `json:"active"`
	Created time.Time `json:"created"`
}
