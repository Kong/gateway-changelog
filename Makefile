test:
	rm -rf kong
	git clone https://github.com/Kong/kong.git
	rm -f 1.0.0.md
	echo "# 1.0.0" > 1.0.0.md
	# use CHANGELOG/unreleased/kong to generate Kong system changelog
	go build && ./changelog generate --repo Kong/kong --repo_path kong --changelog_path CHANGELOG/unreleased/kong --system Kong >> 1.0.0.md
	# use CHANGELOG/unreleased/kong to generate Kong-enterprise system changelog
	go build && ./changelog generate --repo Kong/kong --repo_path kong --changelog_path CHANGELOG/unreleased/kong --system Kong-enterprise >> 1.0.0.md
