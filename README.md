# Zendesk ticket to Zendesk tag with conditional log to S3 blob


This bridge is designed to complete the following event *flow*:

1. Receive _New Ticket_ events from the Zendesk event source.
1. Determine the Ticket's sentiment via [AWS Comprehend][aws-comprehend] using a custom event transformation.
1. IF the ticket is deemed NEGATIVE -> Create a new file in an S3 bucket with the contnts of a negative event
1. Label the new Zendesk Ticket using the Zendesk event target, based on the outcome of the AWS Comprehend sentiment
   analysis.

## Architecture overview


```
+--------+       +--------+       +---------+     +-------+
| source |-------> broker |-------> transf. |<--->|  AWS  |
+--------+       +--------+       +---------+     +-------+
                                       |
                       +---------------+
                       |   POSITIVE
                 +-----v--+       +--------+
                 | broker |-------> target |
                 +--------+       +---^----+
    NEGATIVE        |                 |
    +---------------+                 |
    |                                 |
+---v----+                            |
|  S3    |----------------------------+
+--------+      
```
