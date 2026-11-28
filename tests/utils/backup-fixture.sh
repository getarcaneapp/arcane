# Fills /v with files a backup tool can get wrong, so a restore can be compared
# against the original with backup-manifest.sh.
set -e
cd /v

# Contents: text, a large binary, an empty file, and an empty directory.
mkdir -p dir/sub empty deep/a/b/c/d
printf 'hello\n' > text.txt
head -c 3000000 /dev/urandom > bin.dat
: > empty.txt
printf sub > dir/sub/f.txt
printf deep > deep/a/b/c/d/leaf.txt

# Names that need escaping: spaces, glob characters, unicode, a leading dash.
mkdir -p "space dir" "glob[1]"
printf s > "space dir/file #%.txt"
printf x > "glob[1]/star*?.txt"
printf u > "üñí cödé.txt"
printf d > ./-leading.txt

# Symlinks: relative, absolute, dangling, and to a directory.
ln -s text.txt rel-link
ln -s /etc/hostname abs-link
ln -s missing dangling
ln -s dir dir-link

# Modes, including setuid, setgid, and sticky bits.
mkdir -p sticky setgid
printf suid > dir/suid
printf sgid > dir/sgid
chmod 600 text.txt
chmod 755 bin.dat
chmod 444 empty.txt
chmod 4755 dir/suid
chmod 2755 dir/sgid
chmod 1777 sticky
chmod 2775 setgid
chmod 700 deep
chmod 750 /v

# Owners, including ids with no matching user.
chown 1000:1000 text.txt /v
chown -h 1000:1000 rel-link
chown 12345:54321 bin.dat
chown 65534:65534 dir/sub dir/sub/f.txt
chown 0:1000 setgid

# A fixed mtime, so the manifest shows whether a restore kept it.
find /v -exec touch -h -d "2024-01-02 03:04:05" {} +
