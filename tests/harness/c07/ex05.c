#include <stdio.h>
#include <stdlib.h>

char **ft_split(char *str, char *charset);

int main(int argc, char **argv)
{
	char	**res;
	int		i;

	if (argc != 3)
		return (0);
	res = ft_split(argv[1], argv[2]);
	if (res == NULL)
		return (1);
	i = 0;
	while (res[i] != NULL)
	{
		printf("%s", res[i]);
		if (res[i + 1] != NULL)
			printf("\n");
		i++;
	}
	i = 0;
	while (res[i] != NULL)
		free(res[i++]);
	free(res);
	return (0);
}
