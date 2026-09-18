# Agent Skill 설치 UX 개선

기준: Agent Skills specification + skills.sh ecosystem, 2026-09-18.

## 목표

- 표준 Skill 원본은 `skills/devtools/SKILL.md`를 유지한다.
- 기본 설치를 GitHub repository 기반 한 줄 명령으로 제공한다.
- agent별 설치 경로 선택과 symlink/copy 처리는 `skills` CLI에 맡긴다.
- release tarball은 기본 설치 경로에서 제거하고 offline/manual fallback으로만 문서화한다.
- 공식 `skills-ref` validation과 실제 `skills` CLI repository discovery를 둘 다 CI에서 검증한다.

## 사용자 계약

프로젝트 설치:

```sh
npx skills add jinyongp/devtools
```

전역 설치:

```sh
npx skills add jinyongp/devtools --global
```

특정 skill을 명시해야 하는 자동화:

```sh
npx skills add jinyongp/devtools --skill devtools --yes
```

업데이트:

```sh
npx skills update devtools
# global install
npx skills update devtools --global
```

## 검증

- `skills-ref`가 `skills/devtools`를 Agent Skills 규약에 맞는 것으로 검증.
- exact-pinned `skills` CLI가 repository root에서 `devtools`를 discover.
- public GitHub source `jinyongp/devtools`에서도 `--list`가 `devtools` 하나를 발견하는 것을 확인.
- README와 docs는 tarball/copy 경로보다 repo-native 설치를 먼저 안내.
