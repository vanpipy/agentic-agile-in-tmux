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
  

## Rules

1. the plan wroten by wiki agent who create by others resource - now the wiki did not have a score
2. the coding agent read the plan and base on the exact code repository to evaluate the plan with a
   exact score
3. score >= 90 means the plan has high applicability for the real code repository, otherwise not
4. the coding agent will write the feedback to describe the score(>= 90) and coding agent accepts the plan
   as real guide to coding
5. the coding agent will write the feedback to describe the score(< 90) and the gaps between real and plan
6. so the plan&feedback loop has been built

## How to work

1. in daemon(awp) fire the wiki agent read and copy the plan into workspace from the wiki repository and mark "--- end ---" below the last line in the wiki article
2. then, the daemon(awp) in ticking loop to check the work of wiki agent has done or not, once done
   invoke the coding agent to learn the target code repository and read the plan then let the coding agent write the feedback which include "--- end with {score} ---" below the last line in the feedback article
3. the daemon(awp) will get the "--- end with {score} ---" to make a desision to continue or not.
   Continue 1->2 loop and end to let the coding agent start the coding with the plan
```
