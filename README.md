# Rig — AI-Powered CI/CD Orchestrator

Jenkins의 AI 버전. GitHub 이슈를 받아 AI가 코드를 생성하고, 배포하고, 테스트하고, 실패하면 스스로 분석해서 고치고, PR까지 만드는 자동화 파이프라인.

> **Jenkins가 스크립트를 실행한다면, Rig는 AI가 코드를 짜고, 배포를 분석하고, 인프라까지 수정 제안한다.**

## 핵심 기능

- **이슈 → PR 자동화**: `rig` 라벨이 붙은 이슈를 AI가 자동 처리
- **셀프 힐링**: 테스트 실패 시 AI가 분석 후 코드 수정 → 재시도 (설정 가능)
- **배포 실패 자동 분석**: 배포 실패 시 AI가 인프라 파일(ansible, docker-compose, k8s 등)을 분석하고 수정안 제안
- **제안/승인 시스템**: AI가 제안한 인프라 변경사항을 사람이 검토 후 승인/거부 (안전장치)
- **배포 전 승인 게이트**: `before_deploy: true` 설정 시 배포 전 사람의 승인 필요
- **멀티 프로젝트**: 여러 GitHub 레포를 하나의 Rig 인스턴스에서 관리
- **멀티 AI 프로바이더**: Anthropic (Claude), OpenAI (GPT), Ollama (로컬 LLM), Claude Code CLI
- **유연한 배포**: 로컬 커맨드, SSH 원격 실행 (known_hosts 지원), Docker Compose 지원
- **파이프라인 추적**: 12단계 실행 사이클 + 단계별 상태/에러/타이밍 기록
- **웹 대시보드**: 파이프라인 시각화, 태스크 등록, 제안 Diff 뷰어, 승인/거부 버튼
- **웹훅 서버**: GitHub 이벤트 수신 → 자동 트리거
- **ChatOps**: Slack/Discord에서 `/rig status`, `/rig exec`, `/rig approve` 등 명령어 실행
- **DORA 메트릭스**: 배포 빈도, 리드타임, MTTR, 변경 실패율 자동 계산
- **Policy-as-Code**: AI가 생성한 코드에 규칙 적용 (파일 수 제한, 차단 경로, 테스트 필수 등)
- **AI 실패 분석**: `rig explain --ai` 명령어로 AI가 실패 원인 분석 + 수정 제안
- **스마트 테스트**: `affected_paths` 설정으로 변경된 파일에 관련된 테스트만 실행
- **실시간 로그**: `rig logs --follow` 명령어로 파이프라인 진행 실시간 추적
- **단계별 실행**: `rig exec --step code|deploy|test` 명령어로 특정 단계만 실행
- **Slack/Discord 알림**: 파이프라인 이벤트를 웹훅으로 알림 전송
- **보안 강화**: API 키 인증, CORS 제어, Rate Limiting, 에러 메시지 난독화

## 빠른 시작

### 1. 빌드

```bash
git clone https://github.com/popododo0720/rig.git
cd rig
go build -o rig ./cmd/rig

# 또는
make build
```

### 2. 초기 설정

```bash
# rig.yaml 템플릿 생성
./rig init

# Docker Compose 템플릿
./rig init --template docker
```

### 3. 환경 변수

```bash
export GITHUB_TOKEN="ghp_xxx"           # GitHub Personal Access Token (repo 권한)

# AI Provider (택 1)
export ANTHROPIC_API_KEY="sk-ant-xxx"   # Anthropic API 키
export OPENAI_API_KEY="sk-xxx"          # OpenAI API 키
# Ollama는 API 키 불필요 (로컬 실행)

export WEBHOOK_SECRET="your_secret"     # 웹훅 시그니처 검증용 (선택)

# 보안 (선택)
export RIG_API_KEY="your_api_key"       # 웹 API 인증 키 (미설정시 open access)
export RIG_CORS_ORIGINS="http://localhost:3000"  # CORS 허용 origin (미설정시 same-origin only)
```

### 4. 설정 검증

```bash
./rig validate -c rig.yaml
./rig doctor
```

### 5. 실행

```bash
# 특정 이슈 수동 실행 (GitHub issue URL)
./rig exec https://github.com/owner/repo/issues/42

# dry-run (실제 실행 없이 검증만)
./rig exec https://github.com/owner/repo/issues/42 --dry-run

# 웹훅 서버 시작 (자동 트리거)
./rig run

# 포트 오버라이드
./rig run --port 9000
```

### 6. 상태 확인

```bash
./rig status             # 모든 태스크 조회
./rig logs <task-id>     # 특정 태스크 상세 로그
```

---

## 설정 (`rig.yaml`)

### 최소 설정

```yaml
project:
  name: my-app
  language: go

source:
  platform: github
  repo: owner/repo
  base_branch: main
  token: ${GITHUB_TOKEN}

ai:
  provider: anthropic           # anthropic | openai | ollama
  model: claude-opus-4-6        # 기본값. 또는 claude-sonnet-4-20250514
  api_key: ${ANTHROPIC_API_KEY}
  max_retry: 3

deploy:
  method: custom
  config:
    commands:
      - name: build
        run: "make build"
        timeout: 120s
        transport:
          type: local

test:
  - type: command
    name: unit-test
    run: "make test"
    timeout: 120s

workflow:
  trigger:
    - event: issues.opened
      labels: ["rig"]

server:
  port: 8080
  secret: ${WEBHOOK_SECRET}
```

### AI Provider 설정

**Anthropic (Claude)**
```yaml
ai:
  provider: anthropic
  model: claude-opus-4-6              # 또는 claude-sonnet-4-20250514
  api_key: ${ANTHROPIC_API_KEY}
  max_retry: 3
```

**OpenAI (GPT)**
```yaml
ai:
  provider: openai
  model: gpt-4o                       # 또는 gpt-4o-mini, gpt-4-turbo
  api_key: ${OPENAI_API_KEY}
  max_retry: 3
```

**Ollama (로컬 LLM)**
```yaml
ai:
  provider: ollama
  model: llama3.1                     # 필수 — Ollama에 설치된 모델명
  # api_key: (선택, 기본값 없음)
  max_retry: 3
```

> Ollama는 기본적으로 `http://localhost:11434`에서 실행됩니다.
> 다른 호스트를 사용하려면 환경 변수 설정: `export OLLAMA_API_ENDPOINT="http://remote:11434/v1/chat/completions"`

### 배포 실패 분석 + 승인 설정

```yaml
deploy:
  method: custom
  config:
    commands:
      - name: deploy
        run: "ansible-playbook deploy.yml"
        timeout: 300s
        transport:
          type: local

  # AI가 분석할 인프라 파일 패턴 (glob)
  infra_files:
    - "deploy/*.yml"
    - "docker-compose*.yml"
    - "nginx/*.conf"
    - "k8s/*.yaml"

  # AI가 읽기만 할 수 있는 파일 (수정 제안 불가)
  infra_readonly:
    - "secrets/*.yml"

  # 승인 설정 (인프라 변경은 항상 사람의 승인 필요)
  approval:
    timeout: 24h        # 승인 대기 타임아웃
```

**제안 워크플로우:**

```bash
# 배포 실패 → AI가 제안 생성 → awaiting_approval 상태

# 1. 대기 중인 제안 확인
./rig proposals

# 2. 특정 태스크의 제안 확인
./rig proposals <task-id>

# 3. 제안 승인 (수정 적용 + 재배포)
./rig approve <task-id>

# 4. 제안 거부 (태스크 실패 처리)
./rig reject <task-id>
```

### SSH 원격 배포

```yaml
deploy:
  method: custom
  config:
    commands:
      - name: build-local
        run: "go build -o bin/server ./cmd/server"
        timeout: 120s
        transport:
          type: local

      - name: deploy-remote
        run: "systemctl restart my-app"
        timeout: 300s
        transport:
          type: ssh
          ssh:
            host: 192.168.1.100
            port: 22
            user: deploy
            key: ~/.ssh/deploy_key

  rollback:
    enabled: true
    method: custom
    config:
      commands:
        - name: rollback
          run: "systemctl restart my-app --rollback"
          transport:
            type: ssh
            ssh:
              host: 192.168.1.100
              user: deploy
              key: ~/.ssh/deploy_key
```

### Docker Compose 배포

```yaml
deploy:
  method: docker-compose
  config:
    file: docker-compose.yml
    env_file: .env
```

### 내장 변수

배포/테스트 커맨드에서 `${VAR}` 문법으로 사용 가능:

| 변수 | 설명 |
|------|------|
| `${BRANCH_NAME}` | 자동 생성된 브랜치명 |
| `${COMMIT_SHA}` | 커밋 해시 |
| `${ISSUE_ID}` | 이슈 번호 |
| `${ISSUE_TITLE}` | 이슈 제목 |
| `${REPO_OWNER}` | 레포 소유자 |
| `${REPO_NAME}` | 레포 이름 |

환경 변수도 동일 문법으로 참조: `${GITHUB_TOKEN}`, `${ANTHROPIC_API_KEY}` 등.

### 워크플로우 트리거

```yaml
workflow:
  trigger:
    - event: issues.opened
      labels: ["rig"]              # 이 라벨이 있는 이슈만 처리
    - event: issues.labeled
      labels: ["rig"]
    - event: issue_comment.created
      keyword: "[rig]"             # 코멘트에 키워드가 있으면 트리거
  steps: ["code", "deploy", "test", "report"]
  approval:
    before_deploy: false           # true면 배포 전 승인 필요
```

### 멀티 프로젝트 설정

여러 GitHub 레포를 하나의 Rig 인스턴스에서 관리:

```yaml
projects:
  - name: backend-api
    platform: github
    repo: popododo0720/backend-api
    base_branch: main
  - name: frontend-app
    platform: github
    repo: popododo0720/frontend-app
    base_branch: develop
  - name: infra
    platform: github
    repo: popododo0720/infra
    base_branch: main
```

> 웹 대시보드의 **New Task** 모달에서 프로젝트를 선택하면 해당 레포의 이슈를 바로 처리합니다.

### 알림

```yaml
notify:
  - type: comment                  # GitHub 이슈에 코멘트
    on: ["all"]                    # deploy | test_fail | test_pass | pr_created | all
  - type: slack                    # Slack 웹훅 알림
    webhook: "https://hooks.slack.com/services/T.../B.../xxx"
    on: ["deploy", "test_fail", "pr_created"]
  - type: discord                  # Discord 웹훅 알림
    webhook: "https://discord.com/api/webhooks/xxx/yyy"
    on: ["all"]
```

### 스마트 테스트 (Smart Test Selection)

변경된 파일에 관련된 테스트만 실행하여 시간 절약:

```yaml
test:
  - type: command
    name: api-test
    run: "go test ./api/..."
    timeout: 120s
    affected_paths: ["api/", "internal/api/"]    # 이 경로의 파일이 변경된 경우에만 실행

  - type: command
    name: web-test
    run: "npm test"
    timeout: 60s
    affected_paths: ["web/", "frontend/"]

  - type: command
    name: integration-test
    run: "make integration-test"
    timeout: 300s
    # affected_paths 미설정 = 항상 실행
```

> `affected_paths`가 설정된 테스트는 AI가 생성한 코드 변경이 해당 경로와 매칭될 때만 실행됩니다. 미설정 테스트는 항상 실행됩니다.

### Policy-as-Code (정책 규칙)

AI가 생성한 코드에 자동으로 규칙을 적용:

```yaml
policies:
  - name: file-change-limit
    rule: max_file_changes          # AI가 한번에 수정할 수 있는 최대 파일 수
    value: "10"
    action: block                   # block | warn

  - name: require-tests
    rule: require_tests             # 테스트 설정 필수
    action: block

  - name: protect-secrets
    rule: blocked_paths             # AI가 수정할 수 없는 경로
    value: "*.env,secrets/,.env*"
    action: block

  - name: retry-warning
    rule: max_retries               # 재시도 횟수 경고
    value: "5"
    action: warn
```

> `block` 정책 위반 시 태스크가 즉시 실패합니다. `warn` 정책 위반 시 로그에 경고만 남깁니다.

---

## CLI 명령어

| 명령어 | 설명 | 사용법 |
|--------|------|--------|
| `init` | 설정 템플릿 생성 | `rig init [--template docker]` |
| `validate` | 설정 파일 검증 | `rig validate -c rig.yaml` |
| `exec` | 이슈 수동 실행 | `rig exec <github-issue-url> [--dry-run] [--step code\|deploy\|test] [-c config]` |
| `run` | 웹훅 서버 시작 | `rig run [-p 9000] [-c config]` |
| `status` | 태스크 상태 조회 | `rig status` |
| `logs` | 태스크 로그 조회 | `rig logs <task-id> [--follow]` |
| `explain` | 실패 원인 분석 | `rig explain <task-id> [--ai] [-c config]` |
| `proposals` | 대기 중인 제안 조회 | `rig proposals [task-id]` |
| `approve` | 제안 승인 + 재실행 | `rig approve <task-id> [-c config]` |
| `reject` | 제안 거부 + 태스크 실패 | `rig reject <task-id> [-c config]` |
| `web` | 웹 대시보드 시작 | `rig web [-p 3000] [-c config]` |
| `serve` | 대시보드 + 웹훅 동시 실행 | `rig serve [--web-port 3000] [--webhook-port 9000] [-c config]` |
| `doctor` | 환경 진단 | `rig doctor` |
| `version` | 버전 출력 | `rig version` |

### 새 명령어 상세

**`rig explain <task-id> [--ai]`** — 실패한 태스크의 원인을 분석
```bash
# 구조화된 실패 보고서 (파이프라인, 시도, 제안 정보)
./rig explain task-20250211-001

# AI가 실패 로그를 분석해서 수정 제안까지 제공
./rig explain task-20250211-001 --ai -c rig.yaml
```

**`rig logs <task-id> --follow`** — 실시간 로그 추적
```bash
# 실시간으로 파이프라인 진행 상태를 추적 (2초 간격 폴링)
./rig logs task-20250211-001 --follow
# 완료/실패 시 자동 종료
```

**`rig exec --step`** — 특정 단계만 실행
```bash
# 코드 생성만 실행 (배포/테스트 건너뜀)
./rig exec https://github.com/owner/repo/issues/42 --step code

# 배포만 실행
./rig exec https://github.com/owner/repo/issues/42 --step deploy
```

---

## 실행 사이클

```
  이슈 수신
      │
      ▼
  ┌─────────┐
  │ queued   │
  └────┬─────┘
       ▼
  ┌──────────┐
  │ planning │  AI가 이슈 분석 → 계획 수립
  └────┬──────┘
       ▼
  ┌─────────┐
  │ coding  │  AI가 코드 생성
  └────┬─────┘
       ▼
  ┌───────────┐
  │committing │  브랜치 생성 → 커밋 → 푸시
  └────┬───────┘
       ▼
  ┌───────────┐
  │ deploying │  배포 어댑터 실행
  └────┬───────┘
       │
       ├── 성공 ──────────────────────────┐
       │                                  ▼
       └── 실패 → AI 인프라 분석     ┌──────────┐
                   │                 │ testing  │
                   ▼                 └────┬──────┘
          ┌──────────────────┐            │
          │awaiting_approval │            ├── 통과 → reporting → completed (PR 생성)
          │  (수정안 제안)    │            │
          └────┬─────────────┘            └── 실패 → AI 실패 분석 → 코드 수정
               │                                      → 재배포 → 재테스트
               ├── 승인 → 수정 적용 → 재배포            (max_retry회까지 반복)
               │                                      초과 → rollback → failed
               └── 거부 → failed
```

### 배포 실패 분석 (Deploy Failure Analysis)

배포가 실패하면 AI가 자동으로:
1. 배포 로그를 분석
2. 설정된 인프라 파일(`infra_files`)을 읽음
3. 수정안을 생성하여 **Proposal**로 제안

인프라 변경은 항상 사람의 승인이 필요합니다:

1. AI가 수정안을 **Proposal**로 생성
2. 웹 대시보드 또는 CLI에서 Diff 확인
3. 승인 → 수정 적용 + 재배포 / 거부 → 태스크 실패 처리

---

## GitHub 웹훅 연동

1. 레포 Settings → Webhooks → Add webhook
2. **Payload URL**: `http://your-server:8080/webhook`
3. **Content type**: `application/json`
4. **Secret**: `WEBHOOK_SECRET`와 동일
5. **Events**: Issues 선택
6. 이슈에 `rig` 라벨 → 자동 실행

---

## 프로젝트 구조

```
rig/
├── cmd/rig/                  # CLI
│   ├── main.go               # 루트 커맨드
│   ├── exec.go               # exec (이슈 URL → 전체 사이클)
│   ├── run.go                # run (웹훅 서버)
│   ├── init.go               # init (템플릿 생성)
│   ├── validate.go           # validate
│   ├── status.go             # status
│   ├── logs.go               # logs
│   ├── proposals.go          # proposals (대기 중인 제안 조회)
│   ├── approve.go            # approve (제안 승인 + 재실행)
│   ├── reject.go             # reject (제안 거부)
│   ├── doctor.go             # doctor
│   └── web.go                # web (대시보드 서버)
│
├── internal/
│   ├── config/               # 설정 로딩 + 검증
│   ├── core/                 # 핵심 엔진
│   │   ├── engine.go         # 12단계 오케스트레이터 + 제안/승인 시스템
│   │   ├── workflow.go       # 어댑터 인터페이스 + 단계 함수
│   │   ├── retry.go          # 셀프 힐링 재시도 루프
│   │   ├── infra.go          # 인프라 파일 로딩 헬퍼
│   │   └── state.go          # 상태 머신 + 제안/파이프라인 타입 + JSON 영속화
│   ├── adapter/
│   │   ├── ai/               # AI 어댑터 (Anthropic, OpenAI, Ollama)
│   │   ├── git/              # GitHub API + Git CLI
│   │   ├── deploy/           # 로컬/SSH 커맨드 실행
│   │   ├── test/             # 테스트 러너
│   │   └── notify/           # 알림 (이슈 코멘트)
│   ├── variable/             # ${VAR} 변수 치환
│   ├── web/                  # 웹 대시보드 (go:embed SPA)
│   │   ├── handler.go        # API + 정적 파일 핸들러
│   │   └── static/           # 내장 SPA (HTML/JS/CSS)
│   └── webhook/              # HTTP 서버 + 핸들러
│
├── templates/                # init 템플릿
├── testdata/                 # 테스트 설정 파일
├── rig.yaml.example          # 전체 옵션 예시
├── Makefile
├── Dockerfile
└── go.mod
```

---

## 어댑터 인터페이스

모든 외부 연동은 `internal/core/workflow.go`에 정의된 인터페이스를 구현:

```go
type AIAdapter interface {
    AnalyzeIssue(ctx, issue, projectContext)            (*AIPlan, error)
    GenerateCode(ctx, plan, repoFiles)                  ([]AIFileChange, error)
    AnalyzeFailure(ctx, logs, currentCode)              ([]AIFileChange, error)
    AnalyzeDeployFailure(ctx, deployLogs, infraFiles)   (*AIProposedFix, error)
}

type GitAdapter interface {
    CreateBranch(ctx, branchName)                       error
    CommitAndPush(ctx, changes, message)                error
    CreatePR(ctx, base, head, title, body) (*GitPullRequest, error)
}

type DeployAdapterIface interface {
    Deploy(ctx, vars)  (*AdapterDeployResult, error)
    Rollback(ctx)      error
}

type TestRunnerIface interface {
    Run(ctx, vars) (*TestResult, error)
}
```

새 어댑터 추가:
1. `internal/core/workflow.go`에 인터페이스 확인
2. `internal/adapter/<type>/`에 구현
3. `var _ core.XxxIface = (*MyAdapter)(nil)` 컴파일타임 체크 추가
4. `cmd/rig/exec.go`의 `buildEngineForIssue()`에 와이어링

---

## 웹 대시보드

```bash
# 대시보드 시작 (기본 포트 3000)
./rig web

# 포트 지정
./rig web -p 8888

# 브라우저에서 열기
open http://localhost:3000
```

대시보드 기능:
- 실시간 태스크 상태 모니터링 (SSE)
- **파이프라인 시각화**: 각 단계별 진행 상태 (성공/실패/실행 중/건너뜀)
- **제안 Diff 뷰어**: AI가 제안한 인프라 변경사항을 Before/After로 비교
- **승인/거부 버튼**: 대기 중인 제안을 웹에서 바로 처리
- **태스크 등록**: 웹에서 직접 이슈 URL 입력 → 태스크 생성
- **프로젝트 관리**: 멀티 프로젝트 드롭다운으로 프로젝트별 태스크 분류
- 태스크 상세: 시도 타임라인, 배포 결과, 테스트 결과, 에러 사유
- PR 링크 바로가기
- 다크 테마 (Ink & Ember)

API 엔드포인트:
| 경로 | 설명 |
|------|------|
| `GET /api/tasks` | 전체 태스크 목록 (파이프라인 + 제안 포함) |
| `GET /api/tasks/{id}` | 태스크 상세 |
| `POST /api/tasks` | 새 태스크 생성 (웹에서 이슈 URL 입력) |
| `GET /api/projects` | 등록된 프로젝트 목록 |
| `GET /api/proposals` | 대기 중인 제안 목록 |
| `GET /api/proposals/{taskId}` | 특정 태스크의 대기 중인 제안 |
| `POST /api/approve/{taskId}` | 제안 승인 |
| `POST /api/reject/{taskId}` | 제안 거부 |
| `GET /api/config` | 프로젝트 설정 (민감 정보 제외) |
| `GET /api/events` | SSE 실시간 이벤트 스트림 |
| `GET /api/metrics/dora` | DORA 메트릭스 (30일 기준) |
| `POST /api/chatops/slack` | Slack ChatOps 명령어 수신 |
| `POST /api/chatops/discord` | Discord ChatOps 명령어 수신 |

---

## ChatOps (Slack/Discord)

Slack 또는 Discord에서 직접 Rig를 제어할 수 있습니다.

### Slack 설정

1. Slack App 생성 → Slash Commands → `/rig` 추가
2. **Request URL**: `https://your-server:3000/api/chatops/slack`
3. 사용 가능한 명령어:

```
/rig status          # 태스크 요약 (상태별 카운트)
/rig tasks           # 최근 5개 태스크 목록
/rig logs <task-id>  # 마지막 5개 파이프라인 단계
/rig exec <issue-url># 새 태스크 생성 + 실행
/rig approve <task-id># 제안 승인
/rig reject <task-id> # 제안 거부
```

### Discord 설정

1. Discord webhook 또는 봇에서 `!rig` 프리픽스로 메시지 전송
2. **Webhook URL**: `https://your-server:3000/api/chatops/discord`
3. 동일한 명령어 지원 (`!rig status`, `!rig tasks` 등)

---

## DORA 메트릭스

`GET /api/metrics/dora` 엔드포인트로 최근 30일 기준 DORA 4대 지표를 확인:

```json
{
  "deploy_frequency": 0.5,        // 일 평균 배포 횟수
  "lead_time": 7200000000000,     // 평균 리드타임 (created → completed, nanoseconds)
  "mttr": 3600000000000,          // 평균 복구 시간 (failed → next success, nanoseconds)
  "change_failure_rate": 15.5     // 변경 실패율 (%)
}
```

---

## 보안 설정

### API 키 인증
```bash
export RIG_API_KEY="your-secret-key"
```
설정 시 모든 `/api/*` 엔드포인트에 인증 필요:
- `Authorization: Bearer <key>` 헤더
- `X-API-Key: <key>` 헤더
- `?api_key=<key>` 쿼리 파라미터 (SSE 용)

### CORS
```bash
export RIG_CORS_ORIGINS="http://localhost:3000,https://my-domain.com"
```
미설정 시 same-origin만 허용. `*` 설정 시 모든 origin 허용.

### Rate Limiting
기본 120 요청/분/IP. 초과 시 `429 Too Many Requests` 응답.

### SSH Known Hosts
```yaml
deploy:
  config:
    commands:
      - name: deploy
        run: "systemctl restart app"
        transport:
          type: ssh
          ssh:
            host: 192.168.1.100
            user: deploy
            key: ~/.ssh/deploy_key
            known_hosts: ~/.ssh/known_hosts  # 미설정 시 기본 ~/.ssh/known_hosts 사용, 없으면 검증 건너뜀
```

---

## 개발

```bash
# 빌드
go build -o rig ./cmd/rig

# 전체 테스트
go test ./... -timeout 120s

# 커버리지
go test -cover ./...

# SSH 통합 테스트 (실제 서버 필요)
go test -tags integration -run TestE2E -v ./internal/adapter/deploy/

# 포맷 체크
gofmt -l .
```

## 라이선스

MIT — [LICENSE](LICENSE) 참조
