_mangatool_completion() {
    if (( CURRENT == 2 )); then
        compadd -- convert crop append append-all split split-all rename completion help version --help --version
        return
    fi

    case "$words[2]:$CURRENT" in
        convert:3)
            case "$PREFIX" in
                */*) _files -/ ;;
                *) compadd -- jpg jpeg webp png ;;
            esac
            ;;
        completion:3) compadd -- bash zsh ;;
        convert:4|crop:3|append-all:3|split-all:3|rename:3) _files -/ ;;
        append:*) _files -g '(#i)*.(png|jpg|jpeg)' ;;
        split:*) _files -g '(#i)*.(png|jpg|jpeg|webp)' ;;
    esac
}

compdef _mangatool_completion mangatool
