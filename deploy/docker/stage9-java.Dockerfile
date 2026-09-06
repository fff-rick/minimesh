FROM maven:3.9-eclipse-temurin-21 AS build
WORKDIR /workspace
COPY api /workspace/api
COPY examples/java/order-service /workspace/examples/java/order-service
WORKDIR /workspace/examples/java/order-service
# Keep the demo compatible with Docker's legacy builder. Buildx/BuildKit may
# cache Maven artifacts externally, but a cache mount must not be required just
# to run the local Stage 9/10/13 demos.
RUN mvn -q -DskipTests package

FROM eclipse-temurin:21-jre
COPY --from=build /workspace/examples/java/order-service/target/order-service-0.1.0.jar /app/order-service.jar
ENTRYPOINT ["java", "-cp", "/app/order-service.jar"]
CMD ["io.minimesh.examples.order.OrderServer"]
