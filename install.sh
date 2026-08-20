for dir in fleet-task fleet-cache fleet-run; do
  pushd $dir
  go build -o ~/.local/bin/$dir .
  popd
done
