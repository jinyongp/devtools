#compdef devtools dvt
_devtools() {
  local -a candidates
  local line value description
  while IFS= read -r line; do
    value=${line%%$'\t'*}
    description=''
    [[ $line == *$'\t'* ]] && description=${line#*$'\t'}
    value=${value//\\/\\\\}
    value=${value//:/\\:}
    candidates+=("$value:$description")
  done < <(printf '%s\0' "${(@)words[2,CURRENT]}" | command "${words[1]}" __complete --describe 2>/dev/null)
  (( ${#candidates} )) || return 0
  _describe 'devtools' candidates
}
if [[ $funcstack[1] == _devtools ]]; then
  _devtools "$@"
else
  compdef _devtools devtools dvt
fi
