export type UnionShared =
  | { kind: 'card'; ref: string; last4: string }
  | { kind: 'iban'; ref: string; iban: string };
