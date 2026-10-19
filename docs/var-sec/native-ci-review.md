# Native CI 수정 검토

현재 검증 후보는 main `1035006`이며 release 검증은
[39230873634](https://github.com/jinyongp/devtools/actions/runs/39230873634)에서
`publish=false`로 실행했다. 사용자가 main 진행을 승인했으므로 이후 수정도 main에
반영한다. 이 기록은 실패별 수정과 재검증을 누적하며 native 전체 통과를 뜻하지 않는다.

## Go JSON 결과와 진단 스트림 분리

Linux 서비스 부재 검사는 실제 pass했으나, 처음 다운로드한 Go module의 stderr를
stdout JSONL에 합쳐 Python JSON decoder가 실패했다. stdout만 JSON event로 읽고
stderr는 별도 파일에 보존한다. 같은 결함이 가능한 macOS runner에도 적용한다.
Go 실패 exit, 필수 test 이름의 실제 pass 확인과 skip 거절은 유지한다.

리뷰: 두 runner의 subprocess exit 검사, strict JSON parsing, required-test 집합,
finally cleanup을 재검토했다. Python AST syntax pass. 실제 Linux 정상·잠금·종료 및
macOS locked collection 검사는 수정 revision의 CI에서 다시 확인해야 한다.
