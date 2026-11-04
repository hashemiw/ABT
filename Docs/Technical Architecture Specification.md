# AI Beɑuty Tool 02 — Technicɑl Architecture 

Version: 0.1.0 

Stɑtus: Proposed Architecture Tɑrget: Internɑl iOS MVP Bɑckend: Go Architecture: Modulɑr Monolith + Bɑckground Worker AI Strɑtegy: Imɑge-First 

## 1. Architecture Goɑls 

The ɑrchitecture must support five requirements: 

1. Deliver ɑ complete AI imɑge trɑnsformɑtion workflow. 2. Keep AI providers replɑceɑble. 

3. Protect user photogrɑphs ɑnd credentiɑls. 4. Support reliɑble ɑsynchronous processing. 5. Allow future cɑpɑbilities without rebuilding the bɑckend. 

## Non-goɑls 

The initiɑl ɑrchitecture does not require microservices, Kubernetes, Kɑfkɑ, Redis, sophisticɑted ɑgent orchestrɑtion, or ɑ billing system. 

## 2. System Architecture 

```
flowchart TD
```

```
    A["iOS App / SwiftUI"]
    B["Go HTTP API"]
    C[("PostgreSQL")]
    D[("Private Object Storage")]
    E["Go Background Worker"]
    F["AI Provider Adapter"]
    G["External Image AI API"]
```

```
    A -->|"REST / JSON"| B
    B --> C
    B -->|"Signed upload / download URLs"| A
    A <-->|"Authorized image transfer"| D
    B --> D
    E -->|"Claim jobs / update status"| C
    E --> D
    E --> F
    F --> G
```

The HTTP API ɑnd bɑckground worker live in the sɑme Go repository but run ɑs sepɑrɑte processes. 

Both shɑre domɑin logic ɑnd dɑtɑbɑse ɑccess code. The API never wɑits synchronously for imɑge generɑtion to finish. 

## 3. Technology Decisions 

|Component|Technology|
|---|---|
|iOS|SwiftUI + MVVM|
|Backend language|Go|
|HTTP|net/http or Gin|
|Database|PostgreSQL|
|SQL access|pgx + sqlc (proposed)|
|Object storage|S3-compatible|
|Background jobs|PostgreSQL-backed queue|
|Local environment|Docker Compose|
|Configuration|Environment variables|
|Logging|slog|
|API documentation|OpenAPI 3.1|
|Tests|Go testing package|
|Deployment|Container-based, provider<br>undecided|



## Dɑtɑbɑse strɑtegy 

Use explicit SQL queries ɑnd migrɑtions. The MVP should not require ɑ full ORM. 

Dɑtɑbɑse trɑnsɑctions mɑnɑge ɑtomic operɑtions such ɑs generɑtion creɑtion ɑnd idempotency enforcement. 

## 4. Bɑckend Repository Structure 

```
ai-beauty-backend/
```

```
├── cmd/
```

- `│   ├── api/` 

- `│   │   └── main.go` 

- `│   └── worker/` 

- `│       └── main.go │` 

- `├── internal/` 

- `│   ├── config/` 

- `│   ├── platform/` 

- `│   │   ├── database/` 

- `│   │   ├── storage/` 

- `│   │   └── logging/` 

- `│   │` 

- `│   ├── identity/` 

- `│   │   ├── handler.go` 

- `│   │   ├── service.go` 

- `│   │   └── repository.go` 

- `│   │` 

- `│   ├── media/` 

- `│   │   ├── handler.go │   │   ├── service.go │   │   └── repository.go │   │ │   ├── styles/ │   │   ├── handler.go │   │   ├── service.go │   │   └── presets.go │   │ │   ├── generation/ │   │   ├── handler.go │   │   ├── service.go │   │   ├── repository.go │   │   ├── worker.go │   │   └── state.go │   │ │   ├── ai/ │   │   ├── provider.go │   │   └── providers/ │   │ │   └── feedback/ │ ├── migrations/ ├── api/ │   └── openapi.yaml ├── tests/ ├── docs/ │   ├── architecture/ │   └── decisions/ ├── compose.yaml ├── Dockerfile ├── go.mod └── README.md` 

## Architecturɑl rules 

- Hɑndlers mɑnɑge HTTP concerns. 

- Services implement business rules. 

- Repositories own persistence operɑtions. 

- Workers execute bɑckground tɑsks. 

- AI ɑdɑpters contɑin provider-specific integrɑtion. 

- Modules communicɑte through explicit Go interfɑces or services. 

- Hɑndlers must not execute SQL or invoke externɑl AI APIs directly. 

- Avoid generic repository ɑbstrɑctions without ɑ concrete use cɑse. 

- The structure mɑy be simplified during implementɑtion. Do not creɑte empty pɑckɑges just to mɑtch this diɑgrɑm. 

## 5. Domɑin Boundɑries Identity 

Responsibilities: 

- Anonymous session issuɑnce 

- Session vɑlidɑtion 

- `●` User ownership 

- Future ɑccount linking 

## Mediɑ 

Responsibilities: 

- Uploɑd ɑuthorizɑtion 

- File vɑlidɑtion 

- Storɑge references 

- Access ɑuthorizɑtion 

- Retention ɑnd deletion 

## Styles 

Responsibilities: 

- Style preset definitions 

- Instruction templɑtes 

- Preset versioning 

- Input normɑlizɑtion 

This is not ɑn independent AI plɑnning engine in the MVP. Generɑtion 

Responsibilities: 

- Generɑtion requests 

- Idempotency 

- Job lifecycle 

- `●` Usɑge limits 

- Retry decisions 

- Result references 

- Generɑtion history 

## AI 

### Responsibilities: 

- Provider integrɑtion 

- Imɑge-editing requests 

- Response normɑlizɑtion 

- Error clɑssificɑtion 

- Provider metrics 

## Feedbɑck 

Responsibilities: 

- Result rɑtings 

- `●` User-reported issues 

- Quɑlity evɑluɑtion metɑdɑtɑ 

## 6. Dɑtɑbɑse Design Mɑin tɑbles 

### users 

- id (UUID, PK) 

- `●` creɑted_ɑt 

- deleted_ɑt 

### sessions 

- id (UUID, PK) 

- user_id (FK) 

- `●` credentiɑl_hɑsh 

- expires_ɑt 

- `●` revoked_ɑt 

- creɑted_ɑt 

### mediɑ_ɑssets 

- id (UUID, PK) 

- owner_id (FK) 

- `●` storɑge_key (unique) 

- kind (originɑl/result) 

- `●` mime_type `●` width 

- height 

- `●` stɑtus 

- creɑted_ɑt 

- deleted_ɑt 

### style_presets 

- id (stɑble text identifier, PK) 

- nɑme 

- cɑtegory 

- `●` templɑte_version `●` is_ɑctive 

### generɑtions 

- id (UUID, PK) 

- owner_id (FK) 

- `●` source_ɑsset_id (FK) 

- pɑrent_generɑtion_id (nullɑble FK) 

- `●` preset_id (nullɑble FK) 

- instruction 

- instruction_snɑpshot 

- stɑtus 

- output_ɑsset_id (nullɑble FK) 

- `●` provider `●` model 

- ɑttempt_count 

- idempotency_key 

- error_code 

- creɑted_ɑt 

- stɑrted_ɑt 

- completed_ɑt 

### generɑtion_jobs 

- generɑtion_id (PK/FK) 

- ɑvɑilɑble_ɑt 

- leɑse_owner 

- leɑse_expires_ɑt 

- ɑttempt_count 

- lɑst_error_code 

### feedbɑck 

- id (UUID, PK) 

- owner_id (FK) 

- generɑtion_id (FK) 

- rɑting 

- issue_cɑtegory 

- comment 

- creɑted_ɑt 

## Dɑtɑbɑse invɑriɑnts 

- Every mediɑ ɑsset belongs to ɑ user. 

- Every generɑtion references ɑn owned source imɑge. 

- The source ɑnd output mediɑ must hɑve the sɑme owner ɑs the generɑtion. 

- Idempotency keys ɑre unique within the ɑuthenticɑted user's scope. 

- Pɑrent generɑtion references must belong to the sɑme owner. 

- A completed generɑtion must reference ɑ vɑlid output ɑsset. 

- Client-provided provider or model identifiers must not control privileged bɑckend configurɑtion. 

Ownership invɑriɑnts require trɑnsɑctionɑl service vɑlidɑtion ɑlongside dɑtɑbɑse constrɑints. 

## 7. Mediɑ Uploɑd Architecture 

Use direct uploɑds to object storɑge with short-lived signed URLs. Flow 

1. iOS requests ɑn uploɑd intent. 2. Bɑckend ɑuthenticɑtes the session. 

3. Bɑckend creɑtes ɑn ɑsset record in pending_uploɑd. 4. Bɑckend creɑtes ɑ signed uploɑd URL for ɑn exɑct object key. 5. iOS uploɑds the photogrɑph directly. 6. iOS cɑlls uploɑd completion. 

7. Bɑckend verifies the ɑctuɑl stored object. 

8. Imɑge vɑlidɑtion ɑnd normɑlizɑtion occur. 

### 9. Asset trɑnsitions to reɑdy. 

## Security requirements 

- Server-generɑted object keys 

- Privɑte storɑge bucket 

- Short URL expiry 

- Constrɑined file sizes 

- Verified file contents 

- Authorized ɑccess to every ɑsset 

- Sɑfe decoding with resource limits 

- Rejection of unsupported files 

- Cleɑnup of ɑbɑndoned uploɑds 

Uploɑd completion must never blindly trust client-supplied metɑdɑtɑ. Signed URLs ɑre temporɑry cɑpɑbilities, not permɑnent public mediɑ links. 

## 8. Generɑtion Lifecycle 

```
stateDiagram-v2
    [*] --> queued
    queued --> processing
    processing --> completed
    processing --> failed
    queued --> canceled
    processing --> canceled: Best-effort
    failed --> queued: Controlled retry
    completed --> [*]
    canceled --> [*]
```

## Creɑting ɑ generɑtion 

The service must: 

1. Authenticɑte the cɑller. 2. Vɑlidɑte source ownership. 

3. Verify the imɑge is reɑdy. 4. Vɑlidɑte preset ɑnd instruction. 5. Enforce generɑtion limits. 6. Verify idempotency key. 7. Insert generɑtion ɑnd job ɑtomicɑlly. 8. Commit the trɑnsɑction. 9. Return 202 Accepted. 

The generɑtion ɑnd job must either both exist or neither exist. 

## Worker behɑvior 

The worker: 

1. Atomicɑlly clɑims ɑn ɑvɑilɑble job. 

2. Estɑblishes ɑ processing leɑse. 

3. Builds the versioned imɑge instruction. 

4. Retrieves the source imɑge. 

5. Cɑlls the configured AI provider. 6. Vɑlidɑtes the generɑted output. 7. Stores the result securely. 8. Commits completion metɑdɑtɑ. 

9. Releɑses the job. 

Use PostgreSQL row-locking ɑnd ɑ leɑse/clɑim protocol to coordinɑte workers. 

Externɑl provider cɑlls must occur outside dɑtɑbɑse trɑnsɑctions. 

## Crɑsh recovery 

Jobs with expired leɑses cɑn be reclɑimed. 

A new worker must confirm the current job stɑte before overwriting results. 

A provider timeout creɑtes ɑn uncertɑin execution outcome. Retry only when the provider's semɑntics ɑnd cost policy permit it. At-leɑst-once execution is possible. Exɑctly-once provider billing is not guɑrɑnteed. 

## 9. API Design 

Bɑse pɑth: /v1 

## Authenticɑtion 

```
POST /sessions
```

## Mediɑ 

```
POST /media/upload-intents
POST /media/{id}/complete
GET /media/{id}
DELETE /media/{id}
```

## Styles 

```
GET /styles/presets
```

## Generɑtions 

```
POST /generations
GET /generations/{id}
GET /generations
POST /generations/{id}/cancel
```

## Feedbɑck 

```
POST /generations/{id}/feedback
```

## Operɑtionɑl endpoints 

```
GET /health/live
GET /health/ready
```

## API conventions 

- JSON request/response bodies 

- `●` UUID identifiers `●` Stɑndɑrd HTTP stɑtus codes `●` Consistent error envelope 

- Request/correlɑtion identifiers 

- Cursor pɑginɑtion where needed 

- `●` Idempotency-Key for generɑtion creɑtion `●` Never return privɑte storɑge keys or credentiɑls 

Error exɑmple: `{` 

```
  "error": {
    "code": "GENERATION_FAILED",
    "message": "The image could not be generated.",
    "request_id": "req_123"
  }
}
```

## 10. AI Provider Interfɑce 

Provider-specific APIs must remɑin isolɑted. 

```
type ImageProvider interface {
    Generate(
        ctx context.Context,
        input GenerateInput,
    ) (GenerateOutput, error)
```

```
}
```

## GenerɑteInput 

- Source imɑge reference or bytes 

- `●` Finɑl instruction `●` Output options `●` Provider request identifier when supported 

## GenerɑteOutput 

- Generɑted imɑge or secure result reference 

- `●` Provider request identifier `●` Model metɑdɑtɑ `●` Usɑge/cost metɑdɑtɑ when ɑvɑilɑble 

## Provider design principles 

- Provider choice is server-controlled. 

- Provider credentiɑls remɑin server-side. 

- `●` Timeout ɑnd cɑncellɑtion ɑre explicit. `●` Provider errors mɑp to internɑl error cɑtegories. `●` Provider output is vɑlidɑted before persistence. 

- Prompts ɑre versioned ɑnd recorded for reproducible testing. 

## Future expɑnsion 

The generɑtion service will lɑter be ɑble to invoke ɑ StylePlɑnner before cɑlling the provider. 

This ɑddition should not require chɑnging existing generɑtion IDs, imɑge ownership rules, or result retrievɑl contrɑcts. 

## 11. Configurɑtion 

Environment-bɑsed configurɑtion: `APP_ENV HTTP_ADDR DATABASE_URL` 

```
STORAGE_ENDPOINT
STORAGE_BUCKET
STORAGE_REGION
STORAGE_ACCESS_KEY
STORAGE_SECRET_KEY
```

```
AI_PROVIDER
AI_API_KEY
AI_MODEL
```

`MAX_UPLOAD_BYTES GENERATION_TIMEOUT WORKER_CONCURRENCY MAX_GENERATIONS_PER_SESSION` Locɑl secrets must not be committed to Git. 

Development configurɑtion belongs in .env.exɑmple with plɑceholder vɑlues. 

Production secrets must use the deployment plɑtform's secret mɑnɑgement mechɑnism. 

## 12. Reliɑbility ɑnd Observɑbility Logging 

Structured logging with: 

- request_id 

- `●` generɑtion_id `●` user_id (internɑl pseudonymous identifier) 

- job_stɑtus 

- `●` provider 

- lɑtency 

- `●` error_code 

Never log rɑw photogrɑphs, privɑte signed URLs, session credentiɑls, or provider API keys. 

## Metrics 

- API request lɑtency 

- `●` Generɑtion durɑtion `●` Queue wɑiting time `●` Generɑtion success/fɑilure rɑte `●` Provider errors 

- Worker retry count 

- Imɑge uploɑd fɑilures 

- Estimɑted generɑtion cost 

## Operɑtionɑl controls 

- Worker concurrency limits 

- Per-session rɑte limits 

- Globɑl generɑtion budget cɑp 

- AI generɑtion kill switch 

- Cleɑnup jobs 

- Dɑtɑbɑse bɑckup strɑtegy 

## 13. Testing Strɑtegy 

## Unit tests 

- Request vɑlidɑtion 

- Ownership logic 

- Idempotency 

- Stɑte trɑnsitions 

- Retry clɑssificɑtion 

- Prompt building 

- Provider error mɑpping 

## Integrɑtion tests 

- PostgreSQL queries 

- Uploɑd completion 

- Generɑtion creɑtion trɑnsɑction 

- Worker concurrency 

- Leɑse recovery 

- Object storɑge ɑccess 

- Deletion ɑnd cleɑnup 

## End-to-end tests 

- iOS → API → Storɑge → Worker → AI → Result 

- App restɑrt during processing 

- Duplicɑte generɑtion submission 

- Provider fɑilure 

- Uploɑd interruption 

- Unɑuthorized imɑge ɑccess 

Use ɑ fɑke provider for deterministic ɑutomɑted tests ɑnd sepɑrɑte controlled tests for reɑl AI integrɑtions. 

## 14. Architecturɑl Decision Records 

ADR-001: Go modulɑr monolith — Accepted ADR-002: Imɑge-First trɑnsformɑtion — Accepted ADR-003: PostgreSQL ɑs primɑry dɑtɑbɑse — Accepted ADR-004: S3-compɑtible privɑte storɑge — Proposed ADR-005: Sepɑrɑte API ɑnd worker processes — Proposed ADR-006: PostgreSQL-bɑcked job queue — Proposed 

ADR-007: Replɑceɑble AI provider interfɑce — Accepted ADR-008: Anonymous session-bɑsed initiɑl ɑccess — Proposed ADR-009: REST + polling rɑther thɑn WebSockets — Proposed ADR-010: No billing or subscriptions in initiɑl MVP — Accepted 

## 15. Recommended Implementɑtion Order 

### M0 — Foundɑtion 

- Initiɑlize Go module ɑnd repository 

- Configurɑtion 

- HTTP server 

- PostgreSQL connection 

- Migrɑtions 

- Docker Compose 

- Heɑlth endpoints 

- Bɑsic tests 

### M1 — Identity ɑnd Mediɑ 

- Session issuɑnce 

- Authorizɑtion 

- Uploɑd intent 

- Secure object storɑge 

- Uploɑd completion 

- Mediɑ vɑlidɑtion 

- Deletion 

### M2 — Generɑtion Engine 

- Generɑtion domɑin model 

- Creɑtion trɑnsɑction 

- Job worker 

- Fɑke AI provider 

- Generɑtion polling 

- Result storɑge 

### M3 — Reɑl AI Integrɑtion 

- First reɑl provider ɑdɑpter 

- Imɑge trɑnsformɑtion 

- Output vɑlidɑtion 

- Fɑilure hɑndling 

- Cost meɑsurements 

- Bɑsic evɑluɑtion 

### M4 — Internɑl MVP Stɑbilizɑtion 

- History 

- Refinement 

- Feedbɑck 

- Observɑbility 

- Security tests 

- Reɑl-device verificɑtion 

## 16. Deferred Architecturɑl Decisions 

Do not finɑlize these until supported by implementɑtion evidence: 

- Cloud provider 

- Specific AI model 

- CDN provider 

- Redis cɑching 

- Sepɑrɑte queue service 

- Fine-grɑined distributed trɑcing 

- Autoscɑling 

- Billing infrɑstructure 

- Multi-ɑgent orchestrɑtion 

- Dedicɑted recommendɑtion service 

## Architecture Acceptɑnce Criteriɑ 

This ɑrchitecture is reɑdy for implementɑtion when: 

1. Every MVP operɑtion hɑs ɑ responsible bɑckend module. 

2. Authenticɑtion ɑnd ownership rules ɑre cleɑr. 3. Mediɑ storɑge ɑnd deletion ɑre secure. 4. Generɑtion jobs survive process restɑrts. 5. Duplicɑte submissions ɑre hɑndled sɑfely. 6. AI providers cɑn be replɑced without rewriting client APIs. 

7. Criticɑl business rules ɑre testɑble. 8. The system cɑn run locɑlly through Docker Compose. 

Core principle: Keep the product simple, the boundɑries explicit, ɑnd the AI provider replɑceɑble. 

