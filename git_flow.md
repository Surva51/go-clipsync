cd D:\RE8\transfer\clipboard\clipsync
git init                                        :: create .git/
echo clipsync*.exe        >> .gitignore         :: ignore binaries
echo /vendor/             >> .gitignore         :: (future vendoring)
echo *.log                >> .gitignore
git add .gitignore
git add go.mod go.sum cmd internal              :: stage source tree
git commit -m "v3: first working version (poll + ws)"
