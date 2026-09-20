# pinproc AI provider boundary

pinproc owns the investigation contract. The engine asks small, provider-neutral decision.Question primitives and consumes provider-neutral decision.Answer values.

An AI integration belongs in its own adapter package under internal/decision/. The adapter is responsible for two translations:

1. Canonical pinproc questions -> the provider's HTTP/SDK request schema.
2. Provider responses -> canonical pinproc answers.

The investigation engine must not depend on provider-specific field names, authentication objects, SDK types, endpoints, or response envelopes.

The Jev adapter in internal/decision/jev/ implements the TypeSafe System One payload. Its wire format uses type, instructions, and primitive-specific criteria. Choice criteria are a map, Score criteria are an ordered array, and Noul criteria are an optional true/false map. Responses are translated back to pinproc's Choice, Score, and Noul answers.

To add another AI provider, implement decision.Provider, add its wire conversion layer, and register its configuration name in the CLI/provider factory. The engine, evidence capabilities, report schema, and investigation traversal should remain unchanged.
