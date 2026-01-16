FROM node:24.11.1 AS build-deps

WORKDIR /app/web
COPY ./package.json .
COPY ./package-lock.json .
RUN npm ci  


FROM build-deps AS build

WORKDIR /app/web
COPY . .

# explicitly choose .env.prod
COPY .env.prod .env
RUN npm run build


FROM nginx:latest AS app

COPY --from=build /app/web/out /usr/share/nginx/html
COPY --from=build /app/web/.version.txt /usr/share/nginx/html/version.txt
