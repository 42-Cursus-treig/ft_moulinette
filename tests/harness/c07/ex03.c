#include <stdio.h>
#include <stdlib.h>

char *ft_strjoin(int size, char **strs, char *sep);

int main(int argc, char **argv)
{
	char	*res;
	char	**strs;
	int		size;

	if (argc < 2)
		return (0);
	size = argc - 2;
	strs = argv + 2;
	res = ft_strjoin(size, strs, argv[1]);
	if (res == NULL)
		return (1);
	printf("%s", res);
	free(res);
	return (0);
}
