# N02/A12 Windows x64 emulation installer 실패 진단

d377777의 Windows native x64/ARM64 source suite·다른 SID·native default DPAPI·실제
설치/업데이트와 x64 race가 통과했다. 별도 ARM64의 x64 PowerShell emulation installer
lane은 Invoke-Json에서 실패했으나 artifact stderr는 `null`뿐이었다. 실행 호출 위치와
실제 exit code가 없어 network/installer/실행 오류 중 어느 원인인지 확정하지 않는다.

실패 시에만 engine basename·실제 exit code·테스트 호출 line·최대 16개 option 이름·
stderr byte 수를 별도 context artifact로 남긴다. 값·원문 argv·환경·실제 사용자 자료를
기록하지 않는다. 기존 stderr와 모든 설치/업데이트 assertion을 유지한다. expected-fail
테스트를 성공으로 바꾸거나 외부 다운로드/검증을 자동 재시도하지 않는다.

리뷰: JSON/human 두 helper의 호출 위치, LASTEXITCODE 즉시 저장, 제한된 metadata,
finally 정리 및 always artifact 경로를 확인했다. 새 외부 action/권한 변경은 없다.
source 계약·workflow YAML을 확인하며 실제 emulated engine의 후속 결과로 원인을
판단한다. 이 자료는 N02 native pass를 전체 Windows run pass로 바꾸는 근거가 아니다.
