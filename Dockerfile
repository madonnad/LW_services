FROM golang:1.21
LABEL authors="dominickmadonna"

WORKDIR /app

COPY go.mod go.sum ./

RUN go mod download

COPY . .

RUN go build -o lwServicesBuild ./src

EXPOSE 2626

CMD ["./lwServicesBuild"]