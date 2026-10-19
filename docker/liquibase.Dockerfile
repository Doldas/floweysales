# Liquibase Community 5.x needs the MySQL JDBC driver explicitly installed.
FROM liquibase/liquibase:5.0
RUN lpm add mysql --global
COPY db/changelog/ /liquibase/changelog/
