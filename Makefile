BAZEL = bazel

.PHONY: clean
clean:
	$(BAZEL) clean --expunge

.PHONY: build
build: clean gazelle
	$(BAZEL) build //...
	$(BAZEL) test //...

gazelle-repos:
	dep ensure
	$(BAZEL) run //:gazelle -- update-repos -from_file=Gopkg.lock

gazelle: gazelle-repos
	$(BAZEL) run //:gazelle

test: $(BAZEL) test //...
