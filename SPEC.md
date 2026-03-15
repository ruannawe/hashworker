# Overview

Welcome to the Luxor technical challenge! We expect this challenge to take **6-8 hours** of focused work.

You'll have a **48-hour window** to submit your code, starting from when you receive this challenge. If you need more time due to scheduling constraints, let us know before the deadline.

> NOTE:
> 
> 
> If you are unable to complete this challenge, that's **okay**. But please explain in your submission what you have completed, and let us know your thoughts on the unfinished parts and how you plan to implement them.
> 

## Instructions

Once you've completed your code, submit it via the form you received.

## Ground Rules

The goal is to have functional code to be the basis of a conversation around design trade-offs during the face-to-face interview process.

The most important goal is to create functional code that a colleague would want to review.

Include all `source code`, `test code`, and `a README` describing how to build and run the solution.

This challenge must be completed in **Go**, as it is our primary backend language at Luxor. We believe great engineers can adapt to the right tools for the job, so if you're coming from another language, we encourage you to take on the challenge.

> IMPORTANT:
> 
> 
> We require the code to compile and run successfully on **macOS** or **Linux**.
> 

## Technical Challenge - TCP Message Processing System

Design a TCP-based message processing system with long-lived connections. Your system should:

- Listen for and handle TCP connections.
- Implement the communication protocol detailed below.
- Track and maintain state information.

> NOTE:
> 
> 
> All dependencies (third-party programs) can be started using docker-compose. An example of `docker-compose.yaml` is provided at the bottom. You are not required to use it, but this can save you a lot of time when testing the solution. Feel free to adjust it as needed.
> 

### Components

1. TCP Server
2. TCP Client
3. Message Processor *(Optional bonus)*

## Detailed Requirements

> NOTE:
> 
> 
> Messages requiring responses must include an `id` field to correlate requests and responses.
> 
> For messages not requiring responses, use `id: null`.
> 

### 1. Authentication Flow

**Client -> Server Request:**

```json
{
    "id": 1,
    "method": "authorize",
    "params": {
        "username": "admin"
    }
}

```

**Server -> Client Response:**

```json
{
    "id": 1,
    "result": true
}

```

**Server Requirements:**

- Maintain persistent TCP connections.
- Track `username` per session.
- Support concurrent sessions.
- Validate authentication requests.

**Client Requirements:**

- Single server connection per client instance.
- Maintain persistent connection.
- Wait for server instructions after authentication.

### 2. Task Distribution

**Server -> Client Task Assignment:**

```json
{
    "id": null,
    "method": "job",
    "params": {
        "job_id": 1,
        "server_nonce": "123"
    }
}

```

**Server Requirements:**

- Update `server_nonce` every 30 seconds.
- Broadcast new jobs immediately after nonce updates (and increase the `job_id`).
- Maintain only the latest `server_nonce` per session.
- Maintain a job_id <> server_nonce history for each session to obtain better-detailed error information *(bonus)*.

**Client Requirements:**

- Update local `server_nonce` on job receipt.
- Begin result computation immediately.

### 3. Result Submission

**Client -> Server Submission:**

```json
{
    "id": 2,
    "method": "submit",
    "params": {
        "job_id": 1,
        "client_nonce": "456",
        "result": "8d969eef6ecad3c29a3a629280e686cf0c3f5d5a86aff3ca12020c923adc6c92"
    }
}

```

**Server -> Client Response:**

```json
{
    "id": 2,
    "result": true
}

```

**Or on error:**

```json
{
    "id": 2,
    "result": false,
    "error": "error_message"
}

```

**Client Requirements:**

- Generate random `client_nonce` per submission.
- Calculate: `SHA256(server_nonce + client_nonce)`
- Submit rate: 1/second maximum, 1/minute minimum.
- Maintain submission format.

**Server Requirements:**

- Validate `job_id` and `server_nonce` combinations.
- Verify SHA256 calculations(`SHA256(server_nonce + client_nonce)`).
- Enforce submission rate limits(1/second maximum).
- Detect duplicate client_nonce submissions.

**Error Conditions:**

- Invalid job_id: `"Task does not exist"`.
- Expired job_id(*optional*, could degenerate to `Invalid job_id`): `"Task expired"`.
- Invalid result: `"Invalid result"`.
- Rate limit exceeded: `"Submission too frequent"`.
- Duplicate nonce: `"Duplicate submission"`.

### 4. Statistics Collection

**Database Schema:**

```sql
CREATE TABLE submissions (
    username VARCHAR(255),
    timestamp TIMESTAMP,
    submission_count INT
);

```

**Server Requirements:**

- Track submissions per user.
- Aggregate by minute.
- Store in the database(PostgreSQL).

### 5. Message Processing *(Bonus)*

**Requirements:**

- Implement RabbitMQ integration.
- Process submission events asynchronously.
- Persist statistics in the database.
- Handle server restarts gracefully.

## Technical Notes

1. **SHA256 Calculation:**
    
    ```
    SHA256(server_nonce + client_nonce)
    Example: SHA256("123" + "456") = SHA256("123456")
    
    ```
    
2. **Reference:**
    - Online SHA256 calculator: [This Link](https://emn178.github.io/online-tools/sha256.html)
    - **Input order matters**: SHA256("123456") **≠** SHA256("654321")
3. **Testing:**
    - Use multiple client instances for concurrency testing.
    - Verify all error conditions.
    - Test rate limiting functionality.

## Stuck?

What matters most is seeing how you approach problems. If you get stuck, don't get discouraged — submit your best effort and explain your thinking. That's part of the evaluation.

## Docker Compose Example

Include a `docker-compose.yaml` in your submission. You can use this as a starting point:

```yaml
services:
  rabbitmq:
    image: rabbitmq:4.0-management
    ports:
      - 5672:5672    # RabbitMQ default port
      - 15672:15672  # RabbitMQ management console port, use guest/guest for login
    volumes:
      - rabbitmq_data:/var/lib/rabbitmq

  postgres:
    image: postgres:17
    ports:
      - 5432:5432
    environment:
      POSTGRES_USER: luxor
      POSTGRES_PASSWORD: luxor
      POSTGRES_DB: luxor
    volumes:
      - postgres_data:/var/lib/postgresql/data

volumes:
  rabbitmq_data:
  postgres_data:

```

