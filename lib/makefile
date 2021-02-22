all: libtacle.a

benchmark: benchmark.cpp
	g++ -o $@ $^ -L./ -ltacle

libtacle.a: statemate.o petrinet.o ndes.o mpeg2.o huff_enc.o g723_enc.o epic.o dijkstra.o adpcm_dec.o adpcm_enc.o
	ar -cvq $@ $^

adpcm_dec.o: tacle_benchmarks.h src/adpcm_dec/adpcm_dec.cpp
	g++ -c $^

adpcm_enc.o: tacle_benchmarks.h src/adpcm_enc/adpcm_enc.cpp
	g++ -c $^

dijkstra.o: tacle_benchmarks.h src/dijkstra/dijkstra.cpp
	g++ -c $^

epic.o: tacle_benchmarks.h src/epic/epic.h src/epic/epic.cpp
	g++ -c $^

g723_enc.o: tacle_benchmarks.h src/g723_enc/g723_enc.cpp
	g++ -c $^

huff_enc.o: tacle_benchmarks.h src/huff_enc/huff_enc.cpp
	g++ -c $^

mpeg2.o: tacle_benchmarks.h src/mpeg2/mpeg2.cpp
	g++ -c $^

ndes.o: tacle_benchmarks.h src/ndes/ndes.cpp
	g++ -c $^

petrinet.o: tacle_benchmarks.h src/petrinet/petrinet.cpp
	g++ -c $^

statemate.o: tacle_benchmarks.h src/statemate/statemate.cpp
	g++ -c $^ 

clean:
	rm *.o
	rm libtacle.a