export type Union =
  | { method: 'card'; last4: string }
  | { method: 'iban'; iban: string; bic: string | null };
