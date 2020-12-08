all: libtacle.a

libtacle.a: susan.o statemate.o rijndael_enc.o rijndael_dec.o petrinet.o ndes.o mpeg2.o huff_enc.o huff_dec.o h264_dec.o gsm_enc.o gsm_dec.o g723_enc.o fmref.o epic.o adpcm_dec.o adpcm_enc.o ammunition.o audiobeam.o cjpeg_transupp.o cjpeg_wrbmp.o dijkstra.o
	ar -cvq $@ $^

adpcm_dec.o: tacle_benchmarks.h src/adpcm_dec/adpcm_dec.cpp
	g++ -c $^

adpcm_enc.o: tacle_benchmarks.h src/adpcm_enc/adpcm_enc.cpp
	g++ -c $^

ammunition.o: tacle_benchmarks.h src/ammunition/ammunition.cpp src/ammunition/ammunition_libc.cpp src/ammunition/ammunition_limits.h src/ammunition/ammunition_stdio.h src/ammunition/ammunition_stdlib.h src/ammunition/ammunition_string.h src/ammunition/arithm.cpp src/ammunition/arithm.h src/ammunition/bits.cpp src/ammunition/bits.h 
	g++ -c $^

audiobeam.o: tacle_benchmarks.h src/audiobeam/audiobeam.cpp src/audiobeam/audiobeam.h src/audiobeam/audiobeaminput.cpp src/audiobeam/audiobeamlibm.cpp src/audiobeam/audiobeamlibm.h src/audiobeam/audiobeamlibmalloc.cpp src/audiobeam/audiobeamlibmalloc.h
	g++ -c $^

cjpeg_transupp.o: tacle_benchmarks.h src/cjpeg_transupp/cjpeg_transupp.cpp src/cjpeg_transupp/jpeglib.h
	g++ -c $^

cjpeg_wrbmp.o: tacle_benchmarks.h src/cjpeg_wrbmp/cjpeg_wrbmp.cpp src/cjpeg_wrbmp/input.cpp src/cjpeg_wrbmp/cderror.h src/cjpeg_wrbmp/cdjpeg.h src/cjpeg_wrbmp/jconfig.h src/cjpeg_wrbmp/jerror.h src/cjpeg_wrbmp/jmorecfg.h src/cjpeg_wrbmp/jpeglib.h
	g++ -c $^

dijkstra.o: tacle_benchmarks.h src/dijkstra/dijkstra.cpp src/dijkstra/input.cpp src/dijkstra/input.h
	g++ -c $^

epic.o: tacle_benchmarks.h src/epic/epic.h src/epic/epic.cpp
	g++ -c $^

fmref.o: tacle_benchmarks.h src/fmref/fmref.cpp src/fmref/math_private.h src/fmref/wcclibm.cpp src/fmref/wcclibm.h
	g++ -c $^

g723_enc.o: tacle_benchmarks.h src/g723_enc/g723_enc.cpp
	g++ -c $^

gsm_dec.o: tacle_benchmarks.h src/gsm_dec/add.h src/gsm_dec/data.h src/gsm_dec/gsm.h src/gsm_dec/gsm_dec.cpp src/gsm_dec/private.h src/gsm_dec/gsm.h
	g++ -c $^

gsm_enc.o: tacle_benchmarks.h src/gsm_enc/data.h src/gsm_enc/gsm_enc.cpp src/gsm_enc/private.h src/gsm_enc/gsm.h src/gsm_enc/private.h.gch src/gsm_enc/gsm.h.gch
	g++ -c $^

h264_dec.o: tacle_benchmarks.h src/h264_dec/h264_dec.cpp src/h264_dec/h264_dec.h src/h264_dec/h264_decinput.cpp src/h264_dec/h264_dec.h.gch
	g++ -c $^

huff_dec.o: tacle_benchmarks.h src/huff_dec/huff_dec.cpp
	g++ -c $^

huff_enc.o: tacle_benchmarks.h src/huff_enc/huff_enc.cpp
	g++ -c $^

mpeg2.o: tacle_benchmarks.h src/mpeg2/mpeg2.cpp
	g++ -c $^

ndes.o: tacle_benchmarks.h src/ndes/ndes.cpp
	g++ -c $^

petrinet.o: tacle_benchmarks.h src/petrinet/petrinet.cpp
	g++ -c $^

rijndael_dec.o: tacle_benchmarks.h src/rijndael_dec/aes.cpp src/rijndael_dec/aes.h src/rijndael_dec/aestab.h src/rijndael_dec/input_small_enc.cpp src/rijndael_dec/rijndael_dec.cpp src/rijndael_dec/rijndael_dec_libc.cpp src/rijndael_dec/rijndael_dec_libc.h
	g++ -c $^

rijndael_enc.o: tacle_benchmarks.h src/rijndael_enc/aes.cpp src/rijndael_enc/aes.h src/rijndael_enc/aestab.h src/rijndael_enc/input_small.cpp src/rijndael_enc/rijndael_enc.cpp src/rijndael_enc/rijndael_enc_libc.cpp src/rijndael_enc/rijndael_enc_libc.h
	g++ -c $^

statemate.o: tacle_benchmarks.h src/statemate/statemate.cpp
	g++ -c $^ 

susan.o: tacle_benchmarks.h src/susan/susan.cpp src/susan/input.cpp src/susan/math_private.h src/susan/wccfile.cpp src/susan/wccfile.h src/susan/wcclibm.cpp src/susan/wcclibm.h src/susan/wccmalloc.cpp src/susan/wccmalloc.h
	g++ -c $^

clean:
	rm *.o
	rm libtacle.a