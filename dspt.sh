#!/usr/bin/env bash
# from https://github.com/oneclickvirt/speedtest
# 2024.10.05

speedtest_ver="1.2.0"
speedtest_go_version="1.7.7"
_red() { echo -e "\033[31m\033[01m$@\033[0m"; }
_green() { echo -e "\033[32m\033[01m$@\033[0m"; }
_yellow() { echo -e "\033[33m\033[01m$@\033[0m"; }
_blue() { echo -e "\033[36m\033[01m$@\033[0m"; }
REGEX=("debian|astra" "ubuntu" "centos|red hat|kernel|oracle linux|alma|rocky" "'amazon linux'" "fedora" "arch" "freebsd" "alpine" "openbsd")
RELEASE=("Debian" "Ubuntu" "CentOS" "CentOS" "Fedora" "Arch" "FreeBSD" "Alpine" "OpenBSD")
PACKAGE_UPDATE=("! apt-get update && apt-get --fix-broken install -y && apt-get update" "apt-get update" "yum -y update" "yum -y update" "yum -y update" "pacman -Sy" "pkg update" "apk update" "pkg_add -u")
PACKAGE_INSTALL=("apt-get -y install" "apt-get -y install" "yum -y install" "yum -y install" "yum -y install" "pacman -Sy --noconfirm --needed" "pkg install -y" "apk add")
PACKAGE_REMOVE=("apt-get -y remove" "apt-get -y remove" "yum -y remove" "yum -y remove" "yum -y remove" "pacman -Rsc --noconfirm" "pkg delete" "apk del")
PACKAGE_UNINSTALL=("apt-get -y autoremove" "apt-get -y autoremove" "yum -y autoremove" "yum -y autoremove" "yum -y autoremove" "" "pkg autoremove" "apk autoremove")
CMD=("$(grep -i pretty_name /etc/os-release 2>/dev/null | cut -d \" -f2)" "$(hostnamectl 2>/dev/null | grep -i system | cut -d : -f2)" "$(lsb_release -sd 2>/dev/null)" "$(grep -i description /etc/lsb-release 2>/dev/null | cut -d \" -f2)" "$(grep . /etc/redhat-release 2>/dev/null)" "$(grep . /etc/issue 2>/dev/null | cut -d \\ -f1 | sed '/^[ ]*$/d')" "$(grep -i pretty_name /etc/os-release 2>/dev/null | cut -d \" -f2)" "$(uname -s)" "$(uname -s)")
SYS="${CMD[0]}"
[[ -n $SYS ]] || exit 1
for ((int = 0; int < ${#REGEX[@]}; int++)); do
    if [[ $(echo "$SYS" | tr '[:upper:]' '[:lower:]') =~ ${REGEX[int]} ]]; then
        SYSTEM="${RELEASE[int]}"
        [[ -n $SYSTEM ]] && break
    fi
done

if ! command -v curl >/dev/null 2>&1; then
    _green "Installing curl"
    $PACKAGE_INSTALL curl
fi
if ! command -v tar >/dev/null 2>&1; then
    _green "Installing tar"
    $PACKAGE_INSTALL tar
fi

check_cdn() {
    local o_url=$1
    for cdn_url in "${cdn_urls[@]}"; do
        if curl -sL -k "$cdn_url$o_url" --max-time 6 | grep -q "success" >/dev/null 2>&1; then
            export cdn_success_url="$cdn_url"
            return
        fi
        sleep 0.5
    done
    export cdn_success_url=""
}

check_cdn_file() {
    check_cdn "https://raw.githubusercontent.com/spiritLHLS/ecs/main/back/test"
    if [ -n "$cdn_success_url" ]; then
        echo "CDN available, using CDN"
    else
        echo "No CDN available, no use CDN"
    fi
}

install_speedtest() {
    sys_bit=""
    local sysarch="$(uname -m)"
    case "${sysarch}" in
    "x86_64" | "x86" | "amd64" | "x64") sys_bit="x86_64" ;;
    "i386" | "i686") sys_bit="i386" ;;
    "aarch64" | "armv7l" | "armv8" | "armv8l") sys_bit="aarch64" ;;
    "s390x") sys_bit="s390x" ;;
    "riscv64") sys_bit="riscv64" ;;
    "ppc64le") sys_bit="ppc64le" ;;
    "ppc64") sys_bit="ppc64" ;;
    *) sys_bit="x86_64" ;;
    esac
    download_speedtest_file "${sys_bit}"
}

download_speedtest_file() {
    local sys_bit="$1"
    if ! command -v speedtest >/dev/null 2>&1; then
        _green "Installing speedtest"
        cdn_urls=("https://cdn0.spiritlhl.top/" "http://cdn3.spiritlhl.net/" "http://cdn1.spiritlhl.net/" "http://cdn2.spiritlhl.net/")
        check_cdn_file
        if [ "$speedtest_ver" = "1.2.0" ]; then
            local url1="https://install.speedtest.net/app/cli/ookla-speedtest-1.2.0-linux-${sys_bit}.tgz"
            local url2="https://dl.lamp.sh/files/ookla-speedtest-1.2.0-linux-${sys_bit}.tgz"
        else
            local url1="https://filedown.me/Linux/Tool/speedtest_cli/ookla-speedtest-1.0.0-${sys_bit}-linux.tgz"
            local url2="https://bintray.com/ookla/download/download_file?file_path=ookla-speedtest-1.0.0-${sys_bit}-linux.tgz"
        fi
        curl --fail -sL -m 10 -o speedtest.tgz "${url1}" || curl --fail -sL -m 10 -o speedtest.tgz "${url2}"
        if [[ $? -ne 0 ]]; then
            rm -rf speedtest.tgz*
        fi
        if [ "$sys_bit" = "aarch64" ]; then
            sys_bit="arm64"
        fi
        local url3="https://github.com/showwin/speedtest-go/releases/download/v${speedtest_go_version}/speedtest-go_${speedtest_go_version}_Linux_${sys_bit}.tar.gz"
        curl --fail -sL -m 10 -o speedtest.tar.gz "${url3}" || curl --fail -sL -m 15 -o speedtest.tar.gz "${cdn_success_url}${url3}"
        if [ ! -d "/usr/bin/" ]; then
            mkdir -p "/usr/bin/"
        fi
        if [ -f "speedtest.tgz" ]; then
            tar -zxf speedtest.tgz -C /usr/bin/
            chmod 777 /usr/bin/speedtest
            rm -rf /usr/bin/speedtest.md
            rm -rf /usr/bin/speedtest.5
            rm -rf speedtest.tgz*
        elif [ -f "speedtest.tar.gz" ]; then
            tar -zxf speedtest.tar.gz -C /usr/bin/
            chmod 777 /usr/bin/speedtest-go
            rm -rf /usr/bin/README.md
            rm -rf /usr/bin/LICENSE
            rm -rf speedtest.tar.gz*
        else
            _red "Error: Failed to download speedtest tool."
            exit 1
        fi
    fi
}

install_speedtest_alternative() {
    case $SYSTEM in
    Debian | Ubuntu)
        _green "Installing speedtest using alternative method for Debian/Ubuntu"
        sudo rm /etc/apt/sources.list.d/speedtest.list >/dev/null 2>&1
        sudo apt-get update
        sudo apt-get remove speedtest >/dev/null 2>&1
        sudo apt-get remove speedtest-cli >/dev/null 2>&1
        sudo apt-get install curl -y
        curl -s https://packagecloud.io/install/repositories/ookla/speedtest-cli/script.deb.sh | sudo bash
        sudo apt-get install speedtest -y
        ;;
    CentOS | Fedora)
        _green "Installing speedtest using alternative method for CentOS/Fedora/RHEL"
        sudo rm /etc/yum.repos.d/bintray-ookla-rhel.repo >/dev/null 2>&1
        sudo yum remove speedtest >/dev/null 2>&1
        rpm -qa | grep speedtest | xargs -I {} sudo yum -y remove {} >/dev/null 2>&1
        curl -s https://packagecloud.io/install/repositories/ookla/speedtest-cli/script.rpm.sh | sudo bash
        sudo yum install speedtest -y
        ;;
    FreeBSD)
        _green "Installing speedtest using alternative method for FreeBSD"
        sudo pkg update && sudo pkg install -g libidn2 ca_root_nss
        sudo pkg remove speedtest >/dev/null 2>&1
        if [ "$(uname -r | cut -d'-' -f1)" = "12" ]; then
            sudo pkg add "https://install.speedtest.net/app/cli/ookla-speedtest-1.2.0-freebsd12-x86_64.pkg"
        elif [ "$(uname -r | cut -d'-' -f1)" = "13" ]; then
            sudo pkg add "https://install.speedtest.net/app/cli/ookla-speedtest-1.2.0-freebsd13-x86_64.pkg"
        else
            _red "Unsupported FreeBSD version"
            exit 1
        fi
        ;;
    *)
        _red "Unsupported system for alternative installation method"
        exit 1
        ;;
    esac
}

install_speedtest
if ! speedtest --version >/dev/null 2>&1 || speedtest --version | grep -q "not valid"; then
    _yellow "Standard installation failed or produced invalid output. Trying alternative method..."
    install_speedtest_alternative
fi

speedtest --version

if ! speedtest --version >/dev/null 2>&1; then
    _red "Failed to install speedtest. Please check your system and try again."
    exit 1
fi

_green "Speedtest installation completed successfully."
