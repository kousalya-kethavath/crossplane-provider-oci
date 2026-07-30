# ====================================================================================
# Setup Project

include ../../../build/makelib/common.mk

# ====================================================================================
#  Options

include ../../../build/makelib/imagelight.mk

# ====================================================================================
# Targets

img.build:
	@$(INFO) docker build $(IMAGE)
	@$(MAKE) BUILD_ARGS="--load" img.build.shared
	@$(OK) docker build $(IMAGE)

img.publish:
	@$(INFO) Skipping image publish for $(IMAGE)
	@echo Publish is deferred to xpkg machinery
	@$(OK) Image publish skipped for $(IMAGE)

img.build.shared:
	@cp Dockerfile $(IMAGE_TEMP_DIR) || $(FAIL)
	@set -e; \
	mkdir -p "$(IMAGE_TEMP_DIR)/bin"; \
	for platform in $(subst $(COMMA), ,$(IMAGE_PLATFORMS)); do \
		platform_dir=$$(echo "$$platform" | tr '/' '_'); \
		mkdir -p "$(IMAGE_TEMP_DIR)/bin/$$platform_dir"; \
		cp "$(OUTPUT_DIR)/bin/$$platform_dir/provider" "$(IMAGE_TEMP_DIR)/bin/$$platform_dir/provider" || $(FAIL); \
	done
	@docker buildx build $(BUILD_ARGS) \
		--platform $(IMAGE_PLATFORMS) \
		--build-arg USER_AGENT_PROVIDER_NAME='$(USER_AGENT_PROVIDER_NAME)' \
		-t $(IMAGE) \
		$(IMAGE_TEMP_DIR) || $(FAIL)

img.promote:
	@$(INFO) Skipping image promotion from $(FROM_IMAGE) to $(TO_IMAGE)
	@echo Promote is deferred to xpkg machinery
	@$(OK) Image promotion skipped for $(FROM_IMAGE) to $(TO_IMAGE)
