# Audio-LLM-Contextual - Note : Not part of Heygen the organization. Just a proof of concept to the idea.

Interactive Avatar for Zoom calls : 

A query engine to provide streams of LLM generated Video/Audio to Text, back to Audio with extreme contextual awareness of current conversation using smaller LLM's with lower context window and costs

System Architecture : 
![Screenshot 2024-11-11 at 1 52 20 AM](https://github.com/user-attachments/assets/6c37449a-13e3-4583-8776-a4abfa0e6e3d)

- Integrating this service/concept could lead to increased performance, and a fair cost reduction in certain scenarios.

Prediction Service About : 
- At every query received, it spawns a seperate go routine to store the knowledge acquired in a neo4j DB, to be utilized by other parallel interactive avatars.
- it also spawns a seperate go routine to cache the most related embedding chunks in a redis DB using an adaptive caching mechanism
- For subsequent queries, it searches the cache for relevant information. At every cache miss, it re organizes based on the current query.
- Cache is based on Least-Recently-Used and Least-Frequently-used (TODO) mechanism, holds 5 chunks at a time for a given request.
- Infra wise : Cache can be kept closer to the user's proximity and processed within the cluster.

why?
- Can reduce costs drastically by limiting calls to vector DB and possibly the main LLM model.
- Queries can be redirected based on similarity scores and relevance to smaller LLM models if tested to provide the same accuracy, given the context window.

Additional Feature : 
- Added referencing from audio data (Transcripts) --> Tested with Ted Talks of famous speakers and it gives 95% accuracy compared to textual content like blog data and other highly structured data. --> Tested using Gemma 1.5 pro

Stretch Goal : Integrate advanced audio codecs like AV1 and reduce bitrate for transmission during network congestions, expected in Zoom calls and provides better user experience.
