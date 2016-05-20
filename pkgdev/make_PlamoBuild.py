#! /usr/bin/python
# -*- coding: utf-8 -*-

import argparse, sys, os

def get_args():
    parser = argparse.ArgumentParser(description='PlamoBuild script maker')
    parser.add_argument("-p", "--prefix",  default="/usr", help='install directory prefix(default=/usr)')
    parser.add_argument("-u", "--url", default='input source code url here', help='source code download url')
    parser.add_argument("-v", "--verbose", action="store_true", help="Verbose mode")
    parser.add_argument("-m", "--method", help="force config method(config, cmake, perl, python)")
    parser.add_argument("-s", "--source", action="store_true", help="copy source codes into build directory(use with configure)")
    parser.add_argument("srcdir", help="source code directory")

    return parser.parse_args()

def get_readmes(srcdir):
    keywords = ['ABOUT', 'AUTHOR', 'COPYING', 'CHANGELOG', 'HACKING', 'HISTORY', 'INSTALL', 'LICENSE', 'LSM', 'MAINTAINERS', 'NEWS', 'README', 'RELEASE', 'THANKS', 'THANKYOU', 'TODO',  'TXT']
    exceptions = ['CMakeLists.txt', 'install-sh', 'mkinstalldirs', 'install.sh', '.in']

    tmplist = []
    newlist = [];
    files = os.listdir(srcdir)
    for i in files:
        check = i.upper()
        for j in keywords:
            if check.find(j) >= 0:
                tmplist.append(i)
                break

    for i in tmplist:
        match = False
        for j in exceptions:
            if i.find(j) >= 0:
                match = True
                break
                
        if match == False:
            newlist.append(i)
                
    return newlist

def get_patchfiles(srcdir):
    keywords = ['PATCH', 'DIFF']
    patchlist = []
    files = os.listdir(srcdir)
    for i in files:
        check = i.upper()
        for j in keywords:
            if check.find(j) >= 0:
                patchlist.append(i)

    return patchlist

def get_config_type(srcdir):
    files = os.listdir(srcdir)
    type = 'none'
    for file in files:
        # print file
        if file == 'CMakeLists.txt' :
            type = 'cmake'
            break
        elif file == 'configure' :
            type = 'config'
            break
        elif file == 'setup.py' :
            type = 'python'
            break
        elif file == 'Makefile.PL' :
            type = 'perl'
            break
        
    if type == 'none' :
        print("cannot find proper configure methods.")
        print("PlamoBuild script is setup for configure, but you should adjust build script manually")
        type = 'config'

    return type

def get_basename_and_version(srcdir):
    dirname = srcdir.split('/')[-1]
    parts = dirname.split('-')
    if len(parts) == 2:   # ex: bash-4.02
        basename = parts[0]
        version = parts[1]
    else:                 # ex: kde-baseapps-4.14.3
        basename = "-".join(parts[0:-1])
        version = parts[-1]

    return (basename, version)
    
def make_headers(url, srcdir, pkgbase, vers, readme, patchfiles, method):
    readme.sort()
    docs = " ".join(readme)
    patchs = " ".join(patchfiles)
    
    if method == 'config' :
        header = '''#!/bin/sh
##############################################################
url='{0}'
pkgbase='{1}'
vers='{2}'
arch=`uname -m`
build=P1
src='{3}'
OPT_CONFIG='--disable-static --enable-shared'
DOCS='{4}'
patchfiles='{5}'
compress=txz
##############################################################
'''.format(url, pkgbase, vers, srcdir, docs, patchs)
    else:
        header = '''#!/bin/sh
##############################################################
url='{0}'
pkgbase='{1}'
vers='{2}'
arch=`uname -m`
build=P1
src='{3}'
OPT_CONFIG=''
DOCS='{4}'
patchfiles='{5}'
compress=txz
##############################################################
'''.format(url, pkgbase, vers, srcdir, docs, patchs)

    return header

def make_config(prefix, method, source):
    body_01 = '''
source /usr/lib/setup/plamobuild_functions.sh

if [ $# -eq 0 ] ; then
  opt_download=0 ; opt_config=1 ; opt_build=1 ; opt_package=1
else
  opt_download=0 ; opt_config=0 ; opt_build=0 ; opt_package=0
  for i in $@ ; do
    case $i in
    download) opt_download=1 ;;
    config) opt_config=1 ;;
    build) opt_build=1 ;;
    package) opt_package=1 ;;
    esac
  done
fi
if [ $opt_download -eq 1 ] ; then
    download_sources
fi
'''
    if method == 'config' :
        if source == True :  # with source build
            body_02 = '''
if [ $opt_config -eq 1 ] ; then
    if [ -d $B ] ; then rm -rf $B ; fi ; mkdir -p $B ; cp -a $S/* $B  
######################################################################
#  copy sources into build directory, patch and make in the build dir
######################################################################
    cd $B
    for patch in $patchfiles ; do
       patch -p1 < $W/$patch
    done

    # if [ -f autogen.sh ] ; then
    #   sh ./autogen.sh
    # fi
    export PKG_CONFIG_PATH=/usr/${{libdir}}/pkgconfig:/usr/share/pkgconfig:/opt/kde/${{libdir}}/pkgconfig
    export LDFLAGS='-Wl,--as-needed' 
    export CC="gcc -isystem /usr/include $target" 
    export CXX="g++ -isystem /usr/include $target "
    ./configure --prefix={0} --sysconfdir=/etc --localstatedir=/var --mandir='${{prefix}}'/share/man ${{OPT_CONFIG[$i]}}
    '''.format(prefix)
        else:     # out of source build
            body_02 = '''
if [ $opt_config -eq 1 ] ; then
    if [ -d $B ] ; then rm -rf $B ; fi ; mkdir -p $B 
######################################################################
#  don't copy sources, so need patch in the src dir
######################################################################
    cd $S
    for patch in $patchfiles ; do
        if [ -f .${{patch}} ]; then
            patch -p1 < $W/$patch
            touch .${{patch}}
        fi
    done

    cd $B
    # if [ -f $S/autogen.sh ] ; then
    #   sh ./autogen.sh
    # fi
    export PKG_CONFIG_PATH=/usr/${{libdir}}/pkgconfig:/usr/share/pkgconfig:/opt/kde/${{libdir}}/pkgconfig
    export LDFLAGS='-Wl,--as-needed' 
    export CC="gcc -isystem /usr/include $target" 
    export CXX="g++ -isystem /usr/include $target "
    $S/configure --prefix={0} --sysconfdir=/etc --localstatedir=/var --mandir='${{prefix}}'/share/man ${{OPT_CONFIG[$i]}}
    '''.format(prefix)

    elif method == 'cmake' :      # cmake では out of the tree build がデフォルト
        body_02 = '''
if [ $opt_config -eq 1 ] ; then
    if [ -d $B ] ; then rm -rf $B ; fi ; mkdir -p $B 
######################################################################
#   patch apply to source tree but make at out of source tree
######################################################################
    cd $S
    for patch in $patchfiles ; do
        if [ -f .${{patch}} ]; then
            patch -p1 < $W/$patch
            touch .${{patch}}
        fi
    done
    cd $B
    export PKG_CONFIG_PATH=/usr/${{libdir}}/pkgconfig:/usr/share/pkgconfig:/opt/kde/${{libdir}}/pkgconfig
    export LDFLAGS='-Wl,--as-needed' 
    export CC="gcc -isystem /usr/include $target" 
    export CXX="g++ -isystem /usr/include $target "
    cmake -DCMAKE_INSTALL_PREFIX:PATH={0} ${{OPT_CONFIG[$i]}} $S
    '''.format(prefix)

    elif method == 'python' :
        body_02 = '''
if [ $opt_config -eq 1 ] ; then
    if [ -d $B ] ; then rm -rf $B ; fi ; mkdir -p $B ; cp -a $S/* $B
######################################################################
#  copy srcs to build directory, patch and config in build dir
######################################################################
    cd $B
    for patch in $patchfiles ; do
       patch -p1 < $W/$patch
    done

    python setup.py config
    '''

    elif method == 'perl' :
        body_02 = '''
if [ $opt_config -eq 1 ] ; then
    if [ -d $B ] ; then rm -rf $B ; fi ; mkdir -p $B ; cp -a $S/* $B
######################################################################
#  copy srcs to build directory, patch and config in build dir
######################################################################
    cd $B
    for patch in $patchfiles ; do
       patch -p1 < $W/$patch
    done

    perl Makefile.PL
    '''
    body_03 = '''
    if [ $? != 0 ]; then
        echo "configure error. $0 script stop"
        exit 255
    fi
fi
    '''
    config = body_01 + body_02 + body_03
    return config

def make_build(method):
    if method == 'python':
        body_01 = '''
if [ $opt_build -eq 1 ] ; then
    cd $B
    python setup.py build
    if [ $? != 0 ]; then
        echo "build error. $0 script stop"
        exit 255
    fi
fi

if [ $opt_package -eq 1 ] ; then
  if [ `id -u` -ne 0 ] ; then
    read -p "Do you want to package as root? [y/N] " ans
    if [ "x$ans" == "xY" -o "x$ans" == "xy" ] ; then
      cd $W ; /bin/su -c "$0 package" ; exit
    fi
  fi
  if [ -d $P ] ; then rm -rf $P ; fi ; mkdir -p $P
  if [ -d $C ] ; then rm -rf $C ; fi ; mkdir -p $C
  touch $W/i.st ; sleep 1
  cd $B
  python setup.py install --root $P
'''
    else:
        body_01 = '''
if [ $opt_build -eq 1 ] ; then
    cd $B
    export LDFLAGS='-Wl,--as-needed'
    make -j3
    if [ $? != 0 ]; then
        echo "build error. $0 script stop"
        exit 255
    fi
fi

if [ $opt_package -eq 1 ] ; then
  if [ `id -u` -ne 0 ] ; then
    read -p "Do you want to package as root? [y/N] " ans
    if [ "x$ans" == "xY" -o "x$ans" == "xy" ] ; then
      cd $W ; /bin/su -c "$0 package" ; exit
    fi
  fi
  if [ -d $P ] ; then rm -rf $P ; fi ; mkdir -p $P
  if [ -d $C ] ; then rm -rf $C ; fi ; mkdir -p $C
  touch $W/i.st ; sleep 1
  cd $B
  export LDFLAGS='-Wl,--as-needed'
  make install DESTDIR=$P
'''
    body_02 = '''
################################
#      install tweaks
#  strip binaries, delete locale except ja, compress man, 
#  install docs and patches, compress them and  chown root.root
################################
    install_tweak

  cd $P
  /sbin/makepkg ../$pkg.$compress <<EOF
y
1
EOF

fi
'''
    body = body_01 + body_02
    return body

def main():
    params = get_args()
    cwd = os.getcwd()
    # print params

    if params.method == 'config' :
        method = 'config'
    elif params.method == 'cmake' :
        method = 'cmake'
    elif params.method == 'python' :
        method = 'python'
    elif params.method == 'perl' :
        method = 'perl'
    else :
        method = get_config_type(params.srcdir)

    # print method
    (basename, version) = get_basename_and_version(params.srcdir)
    # print basename, version
    readmes = get_readmes(params.srcdir)
    # print readmes
    patches = get_patchfiles(cwd)
    # print patches

    header = make_headers(params.url, params.srcdir, basename, version, readmes, patches, method)
    config = make_config(params.prefix, method, params.source)
    build = make_build(method)

    script = header + config + build
    script_name = "PlamoBuild." + basename + '-' + version

    if params.verbose :
        print(script)

    if os.path.isfile(script_name) :
        print("same named Build script already exists. ")
        ans = raw_input("OverWrite? [y/N]: ")
        if ans != 'y' and ans != 'Y' :
            sys.exit("not overwrited. exits")

    with open(script_name, 'w') as file:
        file.write(script)

    os.chmod(script_name, 0755)

if __name__ == "__main__":
    main()
