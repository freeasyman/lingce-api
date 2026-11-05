// Package compiler turns a frozen deal-input-v2 snapshot and a structurally
// validated model judgment into a deal-card-v2. It performs no I/O, model calls,
// clock reads or task creation. Source validity is not semantic entailment;
// evaluation of free-text judgments remains a separate responsibility.
package v200
