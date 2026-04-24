FROM node:22-alpine AS build
WORKDIR /src
COPY package.json yarn.lock ./
RUN yarn install --frozen-lockfile
COPY . .
RUN yarn build

FROM caddy:2-alpine
COPY --from=build /src/dist /srv
COPY Caddyfile /etc/caddy/Caddyfile
EXPOSE 3000
