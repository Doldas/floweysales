# Liquibase Community 5.x excludes MySQL's JDBC driver.
# Install a fixed official Connector/J artifact from Maven Central instead of
# LPM: lpm add mysql --global currently fails checksum verification in CI.
FROM liquibase/liquibase:5.0
ARG GIT_COMMIT=""
LABEL org.opencontainers.image.revision="${GIT_COMMIT}" \
      org.opencontainers.image.source="https://github.com/Doldas/floweysales"
# scripts/fetch-liquibase-jdbc.sh prepares a SHA256-verified, cacheable input.
# Do not fetch Maven artifacts during BuildKit ADD: that path caused HTTP 429.
COPY --chmod=0644 docker/vendor/mysql-connector-j-9.5.0.jar /liquibase/internal/lib/mysql-connector-j-9.5.0.jar
COPY docker/mysql-connector-j-9.5.0.sha256 /tmp/mysql-connector.sha256
RUN set -eu; \
    expected="$(cut -d' ' -f1 /tmp/mysql-connector.sha256)"; \
    actual="$(sha256sum /liquibase/internal/lib/mysql-connector-j-9.5.0.jar | cut -d' ' -f1)"; \
    test "$actual" = "$expected"; \
    rm /tmp/mysql-connector.sha256
COPY db/changelog/ /liquibase/changelog/
