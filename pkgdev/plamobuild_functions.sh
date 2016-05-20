install2() {
    install -d ${2%/*} ; install -m 644 $1 $2
}

strip_all() {
    for chk in `find . ` ; do
        chk_elf=`file $chk | grep ELF`
        if [ "$chk_elf.x" != ".x" ]; then
            chk_lib=`echo $chk | grep lib`
            if [ "$chk_lib.x" != ".x" ]; then
                echo "stripping $chk with -g "
                strip -g $chk
            else
                echo "stripping $chk"
                strip $chk
            fi
        fi
    done
}

gzip_dir() {
    echo "compressing in $1"
    if [ -d $1 ] ; then (
        cd $1
        files=`ls -f --indicator-style=none | sed '/^\.\{1,2\}$/d'`
        # files=`ls -a --indicator-style=none | tail -n+3`
        for i in $files ; do
            echo "$i"
            if [ ! -f $i -a ! -h $i -o $i != ${i%.gz} ] ; then continue ; fi
            lnks=`ls -l $i | awk '{print $2}'`
            if [ $lnks -gt 1 ] ; then
                inum=`ls -i $i | awk '{print $1}'`
                for j in `find . -maxdepth 1 -inum $inum` ; do
                    if [ ${j#./} == $i ] ; then
                        gzip -f $i
                    else
                        rm -f ${j#./} ; ln $i.gz ${j#./}.gz
                    fi
                done
            elif [ -h $i ] ; then
                target=`readlink $i` ; rm -f $i ; ln -s $target.gz $i.gz
            else
                gzip $i
            fi
        done
        for i in $files ; do mv ${i%.gz}.gz $C ; done
        for i in $files ; do mv $C/${i%.gz}.gz . ; done
    ) fi
}

gzip_one() {
    gzip $1 ; mv $1.gz $C ; mv $C/${1##*/}.gz ${1%/*}
}


download_sources() {
    for i in $url ; do
        if [ ! -f ${i##*/} ] ; then
            wget $i ;
            for sig in asc sig{,n} {md5,sha{1,256}}{,sum} ; do
                if wget --spider $i.$sig ; then wget $i.$sig ; break ; fi
            done
            if [ -f ${i##*/}.$sig ] ; then
                case $sig in
                    asc|sig) gpg2 --verify ${i##*/}.$sig ;;
                    md5|sha1|sha256) ${sig}sum -c ${i##*/}.$sig ;;
                    *) $sig -c ${i##*/}.$sig ;;
                esac
                if [ $? -ne 0 ] ; then echo "archive verify failed" ; exit ; fi
            fi
        fi
    done
    for i in $url ; do
        case ${i##*.} in
            tar) tar xvpf ${i##*/} ;;
            gz) tar xvpzf ${i##*/} ;;
            bz2) tar xvpjf ${i##*/} ;;
            *) tar xvf ${i##*/} ;;
        esac
    done
}

verify_checksum() {
    echo "Verify Checksum..."
    checksum_command=$1
    verify_file=${verify##*/}
    for s in $url ; do
        srcsum=`$checksum_command ${s##*/}`
        verifysum=`grep ${s##*/} $verify_file`
        if [ x"$srcsum" != x"$verifysum" ]; then
            exit 1
        fi
    done
    exit 0
}


prepare_installdirs() {
    if [ -d $P ] ; then rm -rf $P ; fi ; mkdir -p $P
    if [ -d $C ] ; then rm -rf $C ; fi ; mkdir -p $C
    touch $W/i.st ; sleep 1
}

# インストール後の各種調整
install_tweak() {
    # バイナリファイルを strip
    cd $P
    strip_all 

    # ja 以外のlocaleファイルを削除  
    for loc_dir in `find $P -name locale` ; do
        pushd $loc_dir
        for loc in * ; do
            if [ "$loc" != "ja" ]; then
                rm -rf $loc
            fi
        done
        popd
    done      

    #  man ページを圧縮
    if [ -d $P/usr/share/man ]; then
        for mdir in `find $P/usr/share/man -name man[0-9mno] -type d`; do
            gzip_dir $mdir
        done
    fi

    # doc ファイルのインストールと圧縮  
    cd $W
    for i in `seq 0 $((${#DOCS[@]} - 1))` ; do
        for j in ${DOCS[$i]} ; do
            for k in ${S[$i]}/$j ; do
                install2 $k $docdir/${src[$i]}/${k#${S[$i]}/}
                touch -r $k $docdir/${src[$i]}/${k#${S[$i]}/}
                gzip_one $docdir/${src[$i]}/${k#${S[$i]}/}
            done
        done
        if [ $i -eq 0 ] ; then
            install $myname $docdir/$src
            gzip_one $docdir/$src/$myname
        else
            ln $docdir/$src/$myname.gz $docdir/${src[$i]}
        fi
        ( cd $docdir ; find ${src[$i]} -type d -exec touch -r $W/{} {} \; )
    done

    # パッチファイルのインストール  
    for patch in $patchfiles ; do
        cp $W/$patch $docdir/$src/$patch
        gzip_one $docdir/$src/$patch
    done

    # /usr/share/doc 以下のowner.group設定
    chk_me=`whoami | grep root`
    if [ "$chk_me.x" != ".x" ]; then
        chown -R root.root $P/usr/share/doc
    fi

}

#####
# set working directories

W=`pwd`
for i in `seq 0 $((${#src[@]} - 1))` ; do
    S[$i]=$W/${src[$i]}
    B[$i]=$W/build`test ${#src[@]} -eq 1 || echo $i`
done
P=$W/work ; C=$W/pivot
infodir=$P/usr/share/info
mandir=$P/usr/share/man
xmandir=$P/usr/X11R7/share/man
docdir=$P/usr/share/doc
myname=`basename $0`
pkg=$pkgbase-$vers-$arch-$build

if [ $arch = "x86_64" ]; then
    target="-m64"
    libdir="lib"
    suffix=""
else
    target="-m32"
    libdir="lib"
    suffix=""
fi

