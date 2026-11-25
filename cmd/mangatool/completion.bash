_mangatool_completion() {
    local current="$2" candidate words path_kind
    COMPREPLY=()

    if (( COMP_CWORD == 1 )); then
        words='convert crop append append-all split split-all rename completion help version --help --version'
    else
        case "${COMP_WORDS[1]}:$COMP_CWORD" in
            convert:2) words='jpg jpeg webp png' ;;
            completion:2) words='bash zsh' ;;
            convert:3|crop:2|append-all:2|split-all:2|rename:2) path_kind=directory ;;
            append:*) path_kind=page ;;
            split:*) path_kind=spread ;;
        esac
    fi

    if [[ -n $words ]]; then
        while IFS= read -r candidate; do
            COMPREPLY+=("$candidate")
        done < <(compgen -W "$words" -- "$current")
    elif [[ $path_kind == directory ]]; then
        if type compopt >/dev/null 2>&1; then compopt -o filenames; fi
        while IFS= read -r candidate; do
            COMPREPLY+=("$candidate")
        done < <(compgen -d -- "$current")
    elif [[ -n $path_kind ]]; then
        if type compopt >/dev/null 2>&1; then compopt -o filenames; fi
        while IFS= read -r candidate; do
            if [[ -d $candidate ]]; then
                COMPREPLY+=("$candidate")
            elif [[ -f $candidate ]]; then
                case "$candidate" in
                    *.[pP][nN][gG]|*.[jJ][pP][gG]|*.[jJ][pP][eE][gG]) COMPREPLY+=("$candidate") ;;
                    *.[wW][eE][bB][pP])
                        if [[ $path_kind == spread ]]; then COMPREPLY+=("$candidate"); fi
                        ;;
                esac
            fi
        done < <(compgen -f -- "$current")
    fi

    return 0
}

if type compopt >/dev/null 2>&1; then
    complete -F _mangatool_completion mangatool
else
    complete -o filenames -F _mangatool_completion mangatool
fi
