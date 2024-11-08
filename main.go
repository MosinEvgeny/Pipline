package main

import (
	"bufio"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// Настройки буферизации.
const (
	defaultBufferSize    = 5               // Размер буфера
	defaultFlushInterval = 5 * time.Second // Интервал очистки буфера
)

// RingBuffer - структура для кольцевого буфера.
type RingBuffer struct {
	data []int
	head int
	tail int
	size int
	mu   sync.Mutex
}

// NewRingBuffer - создание нового кольцевого буфера.
func NewRingBuffer(size int) *RingBuffer {
	return &RingBuffer{
		data: make([]int, size),
		size: size,
	}
}

// Push - добавление элемента в буфер.
func (rb *RingBuffer) Push(val int) {
	rb.mu.Lock()
	defer rb.mu.Unlock()

	rb.data[rb.tail] = val
	rb.tail = (rb.tail + 1) % rb.size
	if rb.tail == rb.head {
		rb.head = (rb.head + 1) % rb.size // Перезапись старых данных при переполнении
	}
}

// Flush - получение всех элементов из буфера с очисткой.
func (rb *RingBuffer) Flush() []int {
	rb.mu.Lock()
	defer rb.mu.Unlock()

	if rb.head == rb.tail {
		return nil // Буфер пуст
	}

	data := make([]int, 0, rb.size)
	for rb.head != rb.tail {
		data = append(data, rb.data[rb.head])
		rb.head = (rb.head + 1) % rb.size
	}
	return data
}

// Стадия пайплайна: фильтр отрицательных чисел.
func filterNegative(in <-chan int, out chan<- int, done <-chan bool) {
	defer close(out)
	for {
		select {
		case n := <-in:
			if n >= 0 {
				out <- n
			}
		case <-done:
			return
		}
	}
}

// Стадия пайплайна: фильтр чисел, не кратных 3 (исключая 0).
func filterNotDivisibleBy3(in <-chan int, out chan<- int, done <-chan bool) {
	defer close(out)
	for {
		select {
		case n := <-in:
			if n != 0 && n%3 == 0 {
				out <- n
			}
		case <-done:
			return
		}
	}
}

// Стадия пайплайна: буферизация и периодическая отправка данных.
func bufferAndSend(in <-chan int, out chan<- int, done <-chan bool, bufferSize int, flushInterval time.Duration) {
	defer close(out)
	buffer := NewRingBuffer(bufferSize)
	ticker := time.NewTicker(flushInterval)
	defer ticker.Stop()

	for {
		select {
		case n := <-in:
			buffer.Push(n)
		case <-ticker.C:
			for _, n := range buffer.Flush() {
				out <- n
			}
		case <-done:
			// Очистка буфера перед завершением
			for _, n := range buffer.Flush() {
				out <- n
			}
			return
		}
	}
}

func main() {
	// Обработка прерывания
	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, os.Interrupt, syscall.SIGTERM)

	// Канал для сигнала завершения работы
	done := make(chan bool)
	defer close(done)

	fmt.Println("Программа запущена. Начинайте вводить целые числа:")

	// Источник данных: чтение чисел из консоли
	input := make(chan int)
	go func() {
		defer close(input)
		scanner := bufio.NewScanner(os.Stdin)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if num, err := strconv.Atoi(line); err == nil {
				input <- num
			} else {
				fmt.Println("Некорректный ввод. Введите целое число:")
			}
		}
		fmt.Println("Ввод завершен.")
	}()

	// Создание каналов для пайплайна
	stage1Out := make(chan int)
	stage2Out := make(chan int)
	pipelineOut := make(chan int)

	// Запуск стадий пайплайна
	go filterNegative(input, stage1Out, done)
	go filterNotDivisibleBy3(stage1Out, stage2Out, done)
	go bufferAndSend(stage2Out, pipelineOut, done, defaultBufferSize, defaultFlushInterval)

	fmt.Println("Обработанные данные:")

	// Вывод обработанных данных
	for {
		select {
		case num := <-pipelineOut:
			fmt.Printf("Получены данные: %d\n", num)
		case <-interrupt:
			fmt.Println("\nПрограмма завершена по запросу пользователя.")
			done <- true
			return
		}
	}
}
