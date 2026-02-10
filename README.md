# Rig — AI Dev Agent Orchestrator

GitHub 이슈를 받아 AI가 코드를 생성하고, 배포하고, 테스트하고, PR을 만드는 자동화 에이전트.
실패하면 스스로 분석해서 고치고 다시 시도합니다.

## 핵심 기능

- **이슈 → PR 자동화**: `rig` 라벨이 붙은 이슈를 자동 처리
- **셀프 힐링**: 테스트 실패 시 AI가 분석 후 재시도 (최대 10회)
- **유연한 배포**: 로컬 커맨드, SSH 원격 실행, Docker Compose 지원
- **상태 머신**: 10단계 실행 사이클 + 롤백
- **웹훅 서버**: GitHub 이벤트 수신 → 자동 트리거

## 빠른 시작

### 1. 빌드

```bash
git clone https://github.com/rigdev/rig.git
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
  model: claude-sonnet-4-20250514
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
  model: claude-sonnet-4-20250514     # 또는 claude-3-5-sonnet-20241022
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

### 알림

```yaml
notify:
  - type: comment                  # GitHub 이슈에 코멘트
    on: ["all"]                    # deploy | test_fail | test_pass | pr_created | all
```

---

## CLI 명령어

| 명령어 | 설명 | 사용법 |
|--------|------|--------|
| `init` | 설정 템플릿 생성 | `rig init [--template docker]` |
| `validate` | 설정 파일 검증 | `rig validate -c rig.yaml` |
| `exec` | 이슈 수동 실행 | `rig exec <github-issue-url> [--dry-run] [-c config]` |
| `run` | 웹훅 서버 시작 | `rig run [-p 9000] [-c config]` |
| `status` | 태스크 상태 조회 | `rig status` |
| `logs` | 태스크 로그 조회 | `rig logs <task-id>` |
| `web` | 웹 대시보드 시작 | `rig web [-p 3000] [-c config]` |
| `doctor` | 환경 진단 | `rig doctor` |
| `version` | 버전 출력 | `rig version` |

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
       ▼
  ┌──────────┐
  │ testing  │  테스트 실행
  └────┬──────┘
       │
       ├── 통과 → reporting → completed (PR 생성)
       │
       └── 실패 → AI 실패 분석 → 코드 수정 → 재배포 → 재테스트
                   (max_retry회까지 반복)
                   초과 → rollback → failed
```

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
│   ├── doctor.go             # doctor
│   └── web.go                # web (대시보드 서버)
│
├── internal/
│   ├── config/               # 설정 로딩 + 검증
│   ├── core/                 # 핵심 엔진
│   │   ├── engine.go         # 10단계 오케스트레이터
│   │   ├── workflow.go       # 어댑터 인터페이스 + 단계 함수
│   │   ├── retry.go          # 셀프 힐링 재시도 루프
│   │   └── state.go          # 상태 머신 + JSON 영속화
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
    AnalyzeIssue(ctx, issue, projectContext) (*AIPlan, error)
    GenerateCode(ctx, plan, repoFiles)       ([]AIFileChange, error)
    AnalyzeFailure(ctx, logs, currentCode)   ([]AIFileChange, error)
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
- 태스크 상세: 시도 타임라인, 배포 결과, 테스트 결과
- PR 링크 바로가기
- 다크 테마

API 엔드포인트:
| 경로 | 설명 |
|------|------|
| `GET /api/tasks` | 전체 태스크 목록 |
| `GET /api/tasks/{id}` | 태스크 상세 |
| `GET /api/config` | 프로젝트 설정 (민감 정보 제외) |
| `GET /api/events` | SSE 실시간 이벤트 스트림 |

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
