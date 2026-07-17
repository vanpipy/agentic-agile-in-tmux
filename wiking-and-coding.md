# Interaction

```mermaid
sequenceDiagram
  participant wiking
  participant plan
  participant feedback
  participant coding

  wiking ->> plan: wiki write plan
  coding ->> plan: coding read plan
  coding ->> feedback: coding write feedback
  wiking ->> feedback: wiking read feedback
  
```
