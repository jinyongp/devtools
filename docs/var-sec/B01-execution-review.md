# B01 config·foreground 실행 선택 연결 리뷰

project `bundle` → command `bundle` → CLI `--bundle` 순으로 구체적인 설정이 우선하며 생략은 all이다. all/none/named는 env와 분리되므로 --env가 inject를 활성화해도 none을 all로 바꾸지 않는다. CLI 플래그는 configured name 앞에서만 devtools 옵션이고 이후 --bundle은 그대로 child argument다.

실제 DependenciesFor는 선택을 전달하는 snapshot provider를 사용한다. 기존 전체-profile fixture provider는 all만 허용하며 named 선택을 무시해서 성공하지 않는다. 반환 snapshot의 선택과 실행 선택이 다르면 실패한다. 실제 payload handoff도 선택/metadata 일치를 검사한다. port binding은 선택된 State만 사용하므로 선택 밖 public 변수 참조도 실패한다. proxy 준비 시 config 선택과 foreground CLI override를 같은 capture에 전달한다.

| 영역 | 리뷰·검사 |
| --- | --- |
| project/command 선택·구형 무필드 config | reviewed/pass, TOML 검증·우선순위·--env/none 사례 |
| 실제 public-only provider·slot freeze·handoff | reviewed/pass, 선택된 값만 Injected/metadata에 포함, none 빈 집합, missing 오류 |
| 선택 밖 binding·provider 불일치 | reviewed/pass, 해석 전 선택 제한과 snapshotState/handoff 검사 |
| CLI 인자 경계·proxy 경로·기존 실행 | reviewed/pass, 기존 세 alias child boundary 확장, network prepare caller 확인 |
| 검사 | execution/project/ports/cli 전체 race pass; 각각 3.778/1.109/2.071/42.835s. binding 대상 검사를 추가 후 재검사 pass. 설치된 CLI의 vault/none/missing 선택 시나리오도 pass |

리뷰 중 실제 public fixture의 canonical identity/downgrade marker 누락을 수정했다. 제품 storage 검사를 약화하지 않았다. model/storage 형식은 그대로이며 관리 서버 CLI override 유지·import/일괄 편집·대시보드·문서/Skill 통합은 후속 B01/B03 범위다. 이 하위 작업만으로 B01 완료를 선언하지 않는다.
