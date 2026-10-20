# Liquibase Community 5.x excludes MySQL's JDBC driver.
# Install a fixed official Connector/J artifact from Maven Central instead of
# LPM: lpm add mysql --global currently fails checksum verification in CI.
FROM liquibase/liquibase:5.0
ADD https://repo.maven.apache.org/maven2/com/mysql/mysql-connector-j/9.5.0/mysql-connector-j-9.5.0.jar /liquibase/lib/mysql-connector-j-9.5.0.jar
COPY db/changelog/ /liquibase/changelog/
