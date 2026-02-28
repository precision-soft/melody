#!/bin/bash

if [ -n "${BASH_VERSION:-}" ]; then
    if [[ -f /app/.dev/utility.sh ]]; then
        . /app/.dev/utility.sh
    fi
fi

# generic
alias ll="ls -al"
alias nrd="npm run dev"
alias nrp="npm run prod"
alias nrw="npm run watch"

alias app="cd /app/"
# end generic

if [ -n "${BASH_VERSION:-}" ]; then
    # go
    gv() {
        go vet "$@" ./...
    }

    gt() {
        go test "$@" ./...
    }

    goa() {
        gv "$@"
        gt "$@"
    }

    goa_env_embedded() {
        goa -tags melody_env_embedded "$@"
    }

    goa_static_embedded() {
        goa -tags melody_static_embedded "$@"
    }

    goa_env_and_static_embedded() {
        goa -tags "melody_env_embedded melody_static_embedded" "$@"
    }

    go_build() {
        local outputName="$1"
        shift

        if [ -z "$outputName" ]; then
            echo "missing output name"
            return 1
        fi

        go build -o "$outputName" "$@" .

        local buildExitCode="$?"
        if [ 0 -ne "$buildExitCode" ]; then
            return "$buildExitCode"
        fi

        chmod +x "$outputName"
    }

    go_build_env_embedded() {
        go_build "melody_melody_env_embedded" -tags melody_env_embedded "$@"
    }

    go_build_static_embedded() {
        go_build "melody_melody_static_embedded" -tags melody_static_embedded "$@"
    }

    go_build_env_and_static_embedded() {
        go_build "melody_melody_env_embedded_melody_static_embedded" -tags "melody_env_embedded melody_static_embedded" "$@"
    }

    go_build_all_embedded_modes() {
      go_build "melody_default" "$@"
      go_build_env_embedded "$@"
      go_build_static_embedded "$@"
      go_build_env_and_static_embedded "$@"
    }

    alias gaee="goa_env_embedded"
    alias gase="goa_static_embedded"
    alias gaes="goa_env_and_static_embedded"
    alias gall="gaee && gase && gaes"

    alias gbee="go_build_env_embedded"
    alias gbse="go_build_static_embedded"
    alias gbes="go_build_env_and_static_embedded"
    alias gbam="go_build_all_embedded_modes"
    # end go

    # npm
    snpm() {
        if [[ -e 'package.json' ]]; then
            print_command "npm $*"
            npm "$@"
        else
            error 'package json not found'
            return 0
        fi
    }

    alias npmw="snpm run watch"
    # end npm

    # melody
    melody_validate_all() {
        bash /app/.dev/validate/all.sh --all "$@"
    }

    melody_validate_staged() {
        bash /app/.dev/validate/all.sh --staged "$@"
    }

    melody_install_git_hooks() {
        bash /app/.dev/git-hook/install.sh
    }

    alias mva="melody_validate_all"
    alias mvs="melody_validate_staged"
    alias mhooks="melody_install_git_hooks"
    # end melody

    # git
    sgit() {
        if [[ $(command -v git &>/dev/null) ]]; then
            print_command "git $*"
            git "$@"
        else
            error 'git not found'
            return 0
        fi
    }

    if command -v git > /dev/null 2>&1; then
        git config --global alias.st status
        git config --global alias.ci commit
        git config --global alias.co checkout
        git config --global alias.br branch
        git config --global color.branch auto
        git config --global color.diff auto
        git config --global color.interactive auto
        git config --global color.status auto
        git config --global push.default current
        git config --global init.defaultBranch master
        git config --global core.autocrlf input
        git config --global pull.rebase false
        git config --global --add safe.directory /app/
    else
        warning "git is not installed"
    fi

    gdiff() {
        sgit diff -w "$@"
    }

    alias gdiffc="gdiff --cached"
    # end git

    if [[ -f ~/.bash_aliases_local ]]; then
        . ~/.bash_aliases_local
    fi
fi