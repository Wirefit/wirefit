export interface Presence {
  requiredNonNull: string;
  requiredNullable: string | null;   // present, may be null
  optionalNonNull?: string;          // may be absent, never null
}
