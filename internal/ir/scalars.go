package ir

// Scalar is the canonical cross-language scalar set (SPEC §7).
type Scalar string

const (
	Bool     Scalar = "bool"
	Int32    Scalar = "int32"
	Int64    Scalar = "int64"
	Float32  Scalar = "float32"
	Float64  Scalar = "float64"
	Decimal  Scalar = "decimal"
	String   Scalar = "string"
	Bytes    Scalar = "bytes"
	UUID     Scalar = "uuid"
	Date     Scalar = "date"
	DateTime Scalar = "datetime"
	Duration Scalar = "duration"
)

var allScalars = map[Scalar]string{
	Bool: "boolean", Int32: "integer", Int64: "integer",
	Float32: "number", Float64: "number", Decimal: "number",
	String: "string", Bytes: "string", UUID: "string",
	Date: "string", DateTime: "string", Duration: "string",
}

func (s Scalar) Valid() bool { _, ok := allScalars[s]; return ok }

// JSONType is the wire-level JSON type the scalar serializes to.
func (s Scalar) JSONType() string { return allScalars[s] }

// Fit classifies whether every value of one scalar can be represented by another.
type Fit int

const (
	FitNo    Fit = iota // values exist that the target cannot represent / parse
	FitLossy            // representable but with precision loss (SPEC F7 lossiness warnings)
	FitOK
)

// Fits reports whether values emitted as `from` fit when parsed as `to`.
// Direction matters: Fits(Int32, Int64) is OK, the reverse is not.
func Fits(from, to Scalar) Fit {
	if from == to {
		return FitOK
	}
	switch from {
	case Int32:
		switch to {
		case Int64, Decimal, Float64:
			return FitOK
		case Float32:
			return FitLossy // ints beyond 2^24 lose precision
		}
	case Int64:
		switch to {
		case Decimal:
			return FitOK
		case Float64, Float32:
			return FitLossy // beyond 2^53 (resp. 2^24) unsafe — the JS number case
		}
	case Float32:
		switch to {
		case Float64, Decimal:
			return FitOK
		}
	case Float64:
		switch to {
		case Decimal:
			return FitOK
		case Float32:
			return FitLossy
		}
	case Decimal:
		switch to {
		case Float64, Float32:
			return FitLossy
		}
	case UUID, Date, DateTime, Duration, Bytes:
		if to == String {
			return FitOK // wire format is a string; plain-string consumers parse fine
		}
	}
	return FitNo
}
